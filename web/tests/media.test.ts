import { setLocale } from '../src/lib/i18n'
import { createApp, nextTick } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APIError, ARCHIVE_LIMIT, checkFile, download, MEDIA_LIMIT, upload } from '../src/lib/api'
import { mediaURL, previewKind, reference } from '../src/lib/media'
import type { MediaRecord } from '../src/lib/types'
import MediaView from '../src/components/MediaView.vue'
import { useWorkbenchStore } from '../src/stores/workbench'
import { fakeAPI, response, TestEventSource } from './fixtures'
const media: MediaRecord = { id: 'med_abc', path: 'chats/main/media/med_abc--scene.png', mime: 'image/png', name: 'scene.png', size: 10, sha256: 'hash', preview: true, timeline: 'main' }
const cleanups: Array<() => void> = []
beforeEach(() => { setLocale('zh-CN'); setActivePinia(createPinia()); vi.stubGlobal('fetch', vi.fn(fakeAPI)); vi.stubGlobal('EventSource', TestEventSource) })
afterEach(() => { cleanups.splice(0).forEach((f) => f()); useWorkbenchStore().dispose(); vi.unstubAllGlobals(); vi.restoreAllMocks(); vi.useRealTimers() })
describe('P6 media transport and safe rendering', () => {
  it('restricts preview to safe MIME and URLs to same-origin media IDs', () => {
    for (const mime of ['text/html', 'image/svg+xml', 'application/javascript', 'image/png;bad']) expect(previewKind(mime)).toBeUndefined()
    expect(previewKind('image/png')).toBe('image'); expect(previewKind('audio/wav')).toBe('audio'); expect(previewKind('video/mp4')).toBe('video')
    expect(mediaURL(media)).toBe('/api/v1/media/med_abc')
    expect(mediaURL({ ...media, id: '../project' })).toBe('')
    expect(mediaURL({ ...media, path: 'https://attacker.test/a' }, true)).toBe('/api/v1/media/med_abc?download=1')
    expect(reference(media)).toEqual({ id: media.id, path: media.path, mime: media.mime, name: media.name })
  })
  it('validates empty and oversized files before allocating request body', async () => {
    expect(() => checkFile(new File([], 'empty'), MEDIA_LIMIT)).toThrow('空文件')
    const file = new File(['test'], 'large'); Object.defineProperty(file, 'size', { value: MEDIA_LIMIT + 1 })
    await expect(upload('/media', file)).rejects.toMatchObject({ status: 413 })
    expect(fetch).not.toHaveBeenCalled(); expect(ARCHIVE_LIMIT).toBe(64 * 1024 * 1024)
  })
  it('leaves multipart Content-Type boundary to browser and does not retry uncertain uploads', async () => {
    const file = new File(['image'], 'scene.png')
    vi.mocked(fetch).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(response(media))
    await expect(upload('/media', file, { timeline: 'main' })).rejects.toMatchObject({ code: 'network_error' })
    expect(await upload('/media', file, { timeline: 'main' })).toEqual(media)
    const calls = vi.mocked(fetch).mock.calls
    expect(calls[0]?.[1]?.headers).toEqual(calls[1]?.[1]?.headers)
    expect((calls[0]?.[1]?.headers ?? {})).not.toHaveProperty('Content-Type')
    expect(calls[0]?.[1]?.credentials).toBe('same-origin')
    const body = calls[0]?.[1]?.body as FormData
    expect(body.get('timeline')).toBe('main'); expect(body.get('file')).toBeInstanceOf(File)
  })
  it('shows conflict errors instead of treating failed ZIP import as success', async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify({ error: { code: 'archive_conflict', message: '已有故事 ID' } }), { status: 409 }))
    await expect(upload('/chats/import', new File(['zip'], 'a.zip'), {}, ARCHIVE_LIMIT)).rejects.toMatchObject({ status: 409, message: '已有故事 ID' })
  })
  it('never downloads JSON error envelopes or invalid successful content as ZIP', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'forbidden', message: '登录已过期' } }), { status: 401 })).mockResolvedValueOnce(response({}))
    await expect(download('/project/export', 'p.zip', 'POST')).rejects.toMatchObject({ status: 401 })
    await expect(download('/project/export', 'p.zip', 'POST')).rejects.toMatchObject({ code: 'invalid_download' })
  })
  it('revokes generated download object URLs and sends cookie-authenticated exports', async () => {
    vi.useFakeTimers(); const create = vi.fn(() => 'blob:test'), revoke = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { value: create, configurable: true }); Object.defineProperty(URL, 'revokeObjectURL', { value: revoke, configurable: true })
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    vi.mocked(fetch).mockResolvedValue(new Response('PK zip', { headers: { 'Content-Type': 'application/zip' } }))
    await download('/project/export', 'project.zip', 'POST')
    expect(create).toHaveBeenCalledOnce(); expect(fetch).toHaveBeenCalledWith('/api/v1/project/export', expect.objectContaining({ credentials: 'same-origin', method: 'POST' }))
    await vi.advanceTimersByTimeAsync(1000); expect(revoke).toHaveBeenCalledWith('blob:test')
  })
  it('renders HTML/SVG as download-only, never iframe/object or raw markup', async () => {
    for (const mime of ['text/html', 'image/svg+xml', 'image/png', 'audio/mpeg', 'video/mp4']) {
      const root = document.createElement('div'); document.body.append(root)
      const app = createApp(MediaView, { media: { ...media, mime, name: '<script>alert(1)</script>' } }).use(createPinia()); app.mount(root)
      expect(root.querySelector('iframe,object,embed,script,svg')).toBeNull()
      expect(root.querySelectorAll('img,audio,video').length).toBe(['image', 'video'].includes(previewKind(mime) || '') ? 1 : 0)
      expect(root.querySelector('button')?.type).toBe('button')
      app.unmount(); root.remove()
    }
  })
  it('isolates uploaded attachments across timeline switches and strips record metadata', async () => {
    const s = useWorkbenchStore(); await s.bootstrap()
    vi.mocked(fetch).mockImplementation((url, init) => String(url).endsWith('/media') ? Promise.resolve(response(media)) : fakeAPI(String(url), init))
    expect(await s.uploadAttachment(new File(['image'], 'a.png'))).toBe(true)
    expect(s.draftMedia).toEqual([reference(media)])
    await s.switchTimeline('branch'); expect(s.draftMedia).toEqual([])
    await s.switchTimeline('main'); expect(s.draftMedia).toEqual([reference(media)])
    await s.send()
    const call = vi.mocked(fetch).mock.calls.find(([url]) => String(url).endsWith('/inputs'))
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({ speaker: 'director', text: '', media: [reference(media)] })
    expect(s.draftMedia).toEqual([])
  })
  it('keeps a pending upload in its original timeline, never leaks it to new line', async () => {
    const s = useWorkbenchStore(); await s.bootstrap()
    let resolve!: (r: Response) => void
    vi.mocked(fetch).mockImplementation((url, init) => String(url).endsWith('/media') ? new Promise((r) => { resolve = r }) : fakeAPI(String(url), init))
    const task = s.uploadAttachment(new File(['image'], 'a.png'))
    await s.switchTimeline('branch'); resolve(response(media)); await task; await nextTick()
    expect(s.draftMedia).toEqual([]); await s.switchTimeline('main'); expect(s.draftMedia).toEqual([reference(media)])
    s.report(new APIError(401, 'unauthorized', '登录已过期')); expect(s.draftMedia).toEqual([])
  })
})

it('keeps metadata and downloads in a dismissible media popup, not the chat', async () => {
  const proto = HTMLDialogElement.prototype
  const show = Object.getOwnPropertyDescriptor(proto, 'showModal'), close = Object.getOwnPropertyDescriptor(proto, 'close')
  // jsdom lacks the browser's native dialog methods; only emulate this boundary.
  Object.defineProperty(proto, 'showModal', { configurable: true, value() { this.setAttribute('open', '') } })
  Object.defineProperty(proto, 'close', { configurable: true, value() { this.removeAttribute('open') } })
  const root = document.createElement('div'); document.body.append(root)
  const app = createApp(MediaView, { media }).use(createPinia()); app.mount(root)
  try {
    expect(root.textContent).not.toContain(media.name)
    expect(root.textContent).not.toContain(media.mime)
    expect(root.textContent).not.toContain('下载附件')
    const trigger = root.querySelector('button')!
    trigger.click(); await nextTick()
    const popup = document.querySelector<HTMLDialogElement>('dialog')!
    expect(popup.open).toBe(true)
    expect(popup.textContent).toContain(media.name)
    expect(popup.textContent).toContain(media.mime)
    expect(popup.querySelector('[aria-label="下载附件 scene.png"]')).not.toBeNull()
    popup.dispatchEvent(new Event('cancel', { cancelable: true })); await nextTick()
    expect(document.querySelector('dialog')).toBeNull()
    expect(document.activeElement).toBe(trigger)
  } finally {
    app.unmount(); root.remove()
    if (show) Object.defineProperty(proto, 'showModal', show); else Reflect.deleteProperty(proto, 'showModal')
    if (close) Object.defineProperty(proto, 'close', close); else Reflect.deleteProperty(proto, 'close')
  }
})
