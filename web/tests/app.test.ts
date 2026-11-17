import { setLocale } from '../src/lib/i18n'
import { createApp, nextTick } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../src/App.vue'
import { useWorkbenchStore } from '../src/stores/workbench'
import { fakeAPI, input, project, state, status, timelines } from './fixtures'
const cleanups: Array<() => void> = []
let s: ReturnType<typeof useWorkbenchStore>
function mount(authenticated = true) {
  const pinia = createPinia(); setActivePinia(pinia); s = useWorkbenchStore()
  Object.assign(s, { booting: false, authenticated, project, timelines, status, selectedTimeline: 'main', state })
  const root = document.createElement('div'); document.body.appendChild(root)
  const app = createApp(App).use(pinia); app.mount(root)
  cleanups.push(() => { app.unmount(); root.remove() }); return root
}
function get<T extends Element = HTMLElement>(root: Element, selector: string): T { const e = root.querySelector<T>(selector); if (!e) throw new Error(selector); return e }
beforeEach(() => { setLocale('zh-CN'); vi.stubGlobal('fetch', vi.fn(fakeAPI)); Element.prototype.scrollIntoView = vi.fn() })
afterEach(() => { cleanups.splice(0).forEach((fn) => fn()); vi.unstubAllGlobals() })
describe('P3 business workbench components', () => {
  it('shows an accessible login form without inventing a session', () => { const root = mount(false); expect(get(root, 'input[aria-label="访问令牌"]').getAttribute('type')).toBe('password'); expect(root.querySelector('nav')).toBeNull() })
  it('reacts to the very first draft and identity selection on a new timeline', async () => {
    const root = mount()
    const send = Array.from(root.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent === '发送提示')!
    expect(send.disabled).toBe(true)
    const draft = get<HTMLTextAreaElement>(root, 'textarea[aria-label="创作提示"]')
    draft.value = '首条提示'; draft.dispatchEvent(new Event('input', { bubbles: true }))
    const identity = get<HTMLSelectElement>(root, 'select[aria-label="输入身份"]')
    identity.value = 'chat'; identity.dispatchEvent(new Event('change', { bubbles: true }))
    await nextTick()
    expect(send.disabled).toBe(false); expect(s.draft).toBe('首条提示'); expect(s.inputIdentity).toBe('chat')
  })
  it('always shows inputs, Story and Assistant without display controls', async () => {
    const root = mount(); s.events = [input, { ...input, id: 'evt_s', type: 'output', kind: 'story', text: '**正文**' }, { ...input, id: 'evt_a', type: 'output', kind: 'assistant', text: '讨论' }, { ...input, id: 'evt_c', type: 'character.update' }]
    await nextTick(); expect(root.querySelectorAll('[data-event-id]')).toHaveLength(3)
    expect(get(root, '.markdown-content strong').textContent).toBe('正文')
    expect(root.textContent).not.toContain('显示选项'); expect(root.textContent).not.toContain('阅读排版')
    expect([...root.querySelectorAll('nav button')].some(b => b.textContent?.includes('显示'))).toBe(false)
  })
  it('supports actual management navigation and stable accessible labels', async () => {
    const root = mount()
    const checks = [['worldbook', '搜索世界书'], ['characters', '搜索角色'], ['chats', '搜索故事']] as const
    for (const [panel, label] of checks) { s.activePanel = panel; await nextTick(); expect(root.querySelector(`[aria-label="${label}"]`)).not.toBeNull() }
    expect(root.textContent).not.toContain('并非已实现功能')
  })
  it('keeps the RP node and composer draft when management dialogs change', async () => {
    const root = mount(); s.draft = '保留草稿'; await nextTick(); const history = root.querySelector('[data-testid=history]')
    for (const panel of ['characters', 'worldbook', 'writing'] as const) { s.activePanel = panel; await nextTick(); expect(root.querySelector('[data-testid=history]')).toBe(history); expect(s.draft).toBe('保留草稿') }
  })
  it('renders malicious story HTML as text', async () => {
    const root = mount(); s.events = [{ ...input, type: 'output', kind: 'story', text: '<img src=x onerror=alert(1)>' }]; await nextTick()
    expect(root.querySelector('.markdown-content img')).toBeNull(); expect(root.textContent).toContain('<img src=x onerror=alert(1)>')
  })
  it('does not expose removed plot planning controls', async () => {
    const root = mount(); s.activePanel = 'chats'; await nextTick()
    const save = Array.from(root.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent === '保存确认规划')!
    expect(save).toBeUndefined()
    expect(root.textContent).not.toContain('我明确确认该剧情规划')
  })
})

describe('management safeguards and layered health', () => {
 it('guards direct dialog switches as well as toolbar closure', async () => {
  const root=mount();s.activePanel='chats';await nextTick(); get<HTMLButtonElement>(root, '[aria-label="故事设置 主故事"]').click(); await nextTick()
  const field=get<HTMLInputElement>(root,'input[aria-label="故事名称"]')
  field.value='unsaved';field.dispatchEvent(new Event('input',{bubbles:true}));await nextTick()
  const confirm=vi.fn(()=>false);vi.stubGlobal('confirm',confirm)
  s.activePanel='worldbook';await nextTick();expect(s.activePanel).toBe('chats');expect(confirm).toHaveBeenCalledOnce()
  confirm.mockReturnValue(true);s.activePanel='writing';await nextTick();expect(s.activePanel).toBe('writing')
 })
})


it('retains unsaved protection after a failed save and an unrelated success notice', async () => {
 const root=mount();s.activePanel='chats';await nextTick(); get<HTMLButtonElement>(root, '[aria-label="故事设置 主故事"]').click(); await nextTick()
 const field=get<HTMLInputElement>(root,'input[aria-label="故事名称"]');field.value='unsaved';field.dispatchEvent(new Event('input',{bubbles:true}))
 vi.stubGlobal('fetch',vi.fn(()=>Promise.resolve(new Response(JSON.stringify({error:{code:'rejected',message:'save rejected'}}),{status:422}))))
 field.closest('form')!.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}))
 await new Promise(resolve=>setTimeout(resolve,10));expect(s.error).toContain('rejected')
 await nextTick()
 const confirm=vi.fn(()=>false);vi.stubGlobal('confirm',confirm);s.activePanel='writing';await nextTick()
 expect(confirm).toHaveBeenCalledOnce();expect(s.activePanel).toBe('chats')
})


describe('management modal behavior', () => {
 it('opens every management entry as a labelled modal and disables the whole background', async () => {
  const root = mount(), background = get(root, '.workbench-content')
  for (const panel of ['worldbook', 'characters', 'chats'] as const) {
   s.activePanel = panel; await nextTick()
   const dialog = get(root, '[role="dialog"]')
   expect(dialog.getAttribute('aria-modal')).toBe('true')
   expect(dialog.getAttribute('aria-labelledby')).toBe('management-title')
   expect(get(dialog, '#management-title').textContent).toBeTruthy()
   expect(background.hasAttribute('inert')).toBe(true)
   expect(background.contains(dialog)).toBe(false)
  }
  s.activePanel = 'writing'; await nextTick()
  expect(root.querySelector('[role="dialog"]')).toBeNull()
  expect(background.hasAttribute('inert')).toBe(false)
 })
 it('restores trigger focus after Escape and closes only direct backdrop clicks', async () => {
  const root = mount(), trigger = [...root.querySelectorAll<HTMLButtonElement>('nav button')].find(b => b.textContent?.includes('世界书'))!
  trigger.focus(); trigger.click(); await nextTick(); await nextTick()
  const dialog = get(root, '[role="dialog"]')
  expect(document.activeElement).toBe(get(dialog, '.modal-close'))
  dialog.dispatchEvent(new MouseEvent('click', { bubbles: true })); await nextTick()
  expect(s.activePanel).toBe('worldbook')
  dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); await nextTick(); await nextTick()
  expect(s.activePanel).toBe('writing'); expect(document.activeElement).toBe(trigger)
  trigger.click(); await nextTick()
  get(root, '.modal-backdrop').dispatchEvent(new MouseEvent('click', { bubbles: true })); await nextTick()
  expect(s.activePanel).toBe('writing')
 })
 it('protects dirty edits on Escape, backdrop and close button until discard is accepted', async () => {
  const root = mount(); s.activePanel = 'chats'; await nextTick(); get<HTMLButtonElement>(root, '[aria-label="故事设置 主故事"]').click(); await nextTick()
  const field = get<HTMLInputElement>(root, '[aria-label="故事名称"]')
  field.value = '未保存名称'; field.dispatchEvent(new Event('input', { bubbles: true })); await nextTick()
  const confirm = vi.fn(() => false); vi.stubGlobal('confirm', confirm)
  get(root, '[role="dialog"]').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); await nextTick()
  get(root, '.modal-backdrop').dispatchEvent(new MouseEvent('click', { bubbles: true })); await nextTick()
  get<HTMLButtonElement>(root, '.modal-close').click(); await nextTick()
  expect(confirm).toHaveBeenCalledTimes(3); expect(field.value).toBe('未保存名称'); expect(s.activePanel).toBe('chats')
  confirm.mockReturnValue(true); get<HTMLButtonElement>(root, '.modal-close').click(); await nextTick()
  expect(s.activePanel).toBe('writing')
 })
 it('shows save errors inside the active modal rather than behind its backdrop', async () => {
  const root = mount(); s.activePanel = 'chats'; s.error = '保存失败，请重试'; await nextTick()
  expect(root.querySelectorAll('[role="alert"]')).toHaveLength(0)
  expect(document.querySelector('.error-snackbar')?.textContent).toContain('保存失败，请重试')
 })
})


describe('output editing and deletion', () => {
 const output = { ...input, id: 'evt_output', type: 'output', kind: 'story', text: '原文' }
 it('edits output by position and retains draft on failure', async () => {
  const root = mount(); s.events = [input, output]; await nextTick()
  expect(root.querySelector(`[aria-label="编辑输出 ${input.id}"]`)).toBeNull()
  get<HTMLButtonElement>(root, '[aria-label="编辑输出 evt_output"]').click(); await nextTick()
  const editor = get<HTMLTextAreaElement>(root, '[aria-label="编辑正文 evt_output"]')
  editor.value = '修改后'; editor.dispatchEvent(new Event('input', { bubbles: true })); await nextTick()
  const mutate = vi.spyOn(s, 'mutate').mockResolvedValue(false)
  editor.closest('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); await nextTick(); await nextTick()
  expect(mutate).toHaveBeenCalledWith('/chats/main/events/evt_output', 'PATCH', { text: '修改后' })
  expect(editor.value).toBe('修改后')
  mutate.mockResolvedValue(true)
  editor.closest('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); await nextTick(); await nextTick()
  expect(root.querySelector('[aria-label="编辑正文 evt_output"]')).toBeNull()
 })
 it('keeps unsaved output drafts isolated when switching timelines', async () => {
  const root = mount(); s.events = [output]; await nextTick()
  get<HTMLButtonElement>(root, '[aria-label="编辑输出 evt_output"]').click(); await nextTick()
  const editor = get<HTMLTextAreaElement>(root, '[aria-label="编辑正文 evt_output"]')
  editor.value = 'main草稿'; editor.dispatchEvent(new Event('input', { bubbles: true })); await nextTick()
  s.selectedTimeline = 'branch'; await nextTick(); expect(root.querySelector('[aria-label="编辑正文 evt_output"]')).toBeNull()
  s.selectedTimeline = 'main'; await nextTick(); expect(get<HTMLTextAreaElement>(root, '[aria-label="编辑正文 evt_output"]').value).toBe('main草稿')
 })
})

describe('concise workbench copy', () => {
 it('removes protocol explanations from the composer and management panels', async () => {
  const root = mount()
  for (const identity of ['chat', 'director']) {
   s.inputIdentity = identity; await nextTick()
   expect(root.querySelector('.composer-kind')).toBeNull()
   expect(root.textContent).not.toMatch(/→ Story|→ Assistant|永久保留|Agent 通过 CLI/)
  }
  for (const panel of ['characters', 'worldbook', 'chats'] as const) {
   s.activePanel = panel; await nextTick()
   expect(get(root, '[role="dialog"]').textContent).not.toMatch(/WORKSPACE|不属于小说正文|不会假成功|不保存故事级覆盖|故事之间没有继承关系|Thread 当作账号/)
  }
 })
 it('retains consequential editing, settings and upload guidance', async () => {
  const root = mount()
  s.activePanel = 'writing'
  s.events = [{ ...input, id: 'evt_output', type: 'output', kind: 'story', text: '正文' }]
  await nextTick(); get<HTMLButtonElement>(root, '[aria-label="编辑输出 evt_output"]').click(); await nextTick()
  expect(root.textContent).toContain('保存后替换原文，不自动重新生成回复，也不改变角色状态、附件和后续剧情')
  const confirm = vi.fn(() => false); vi.stubGlobal('confirm', confirm)
  get<HTMLButtonElement>(root, '[aria-label="删除至输出 evt_output"]').click()
  expect(confirm).toHaveBeenCalledWith(expect.stringContaining('永久删除从最新消息到这条消息之间的全部内容（包含本条）'))
 })
})



describe('history input actions and independent branches', () => {
 it('offers edit/delete/branch for both kinds without rendering internal IDs', async () => {
  const root = mount()
  s.events = [input, { ...input, id: 'evt_reply', type: 'output', kind: 'story', text: '回复' }]
  await nextTick()
  for (const [id, kind] of [[input.id, '输入'], ['evt_reply', '输出']]) {
   const message = get(root, `[data-event-id="${id}"]`)
   expect(message.querySelector(`[aria-label="编辑${kind} ${id}"]`)).not.toBeNull()
   expect(message.querySelector(`[aria-label="删除至${kind} ${id}"]`)).not.toBeNull()
   expect(message.querySelector(`[aria-label="从此处分支 ${id}"]`)).not.toBeNull()
   expect(message.textContent).not.toContain(id)
   expect(message.textContent).not.toMatch(/事件：|请求：/)
  }
 })
 it('edits input using the displayed position and confirms cascading deletion', async () => {
  const root = mount(); s.events = [{ ...input, position: 1 }]; await nextTick()
  expect(root.textContent).not.toContain('已编辑')
  get<HTMLButtonElement>(root, `[aria-label="编辑输入 ${input.id}"]`).click(); await nextTick()
  const editor = get<HTMLTextAreaElement>(root, `[aria-label="编辑正文 ${input.id}"]`)
  editor.value = '修订后的输入'; editor.dispatchEvent(new Event('input', { bubbles: true }))
  const mutate = vi.spyOn(s, 'mutate').mockResolvedValue(false)
  editor.closest('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); await nextTick()
  expect(mutate).toHaveBeenCalledWith('/chats/main/events/1', 'PATCH', { text: '修订后的输入' })
  const confirm = vi.fn(() => false); vi.stubGlobal('confirm', confirm)
  get<HTMLButtonElement>(root, `[aria-label="删除至输入 ${input.id}"]`).click(); await nextTick()
  expect(confirm).toHaveBeenCalledWith(expect.stringContaining('从最新消息到这条消息之间的全部内容（包含本条）'))
  expect(mutate).toHaveBeenCalledTimes(1)
 })
 it('allows copying an archived story but not editing or deleting its messages', async () => {
  const root = mount(); s.timelines = [{ ...timelines[0]!, archived: true }]; s.events = [input]; await nextTick()
  expect(get<HTMLButtonElement>(root, `[aria-label="编辑输入 ${input.id}"]`).disabled).toBe(true)
  expect(get<HTMLButtonElement>(root, `[aria-label="删除至输入 ${input.id}"]`).disabled).toBe(true)
  expect(get<HTMLButtonElement>(root, `[aria-label="从此处分支 ${input.id}"]`).disabled).toBe(false)
 })
})

it('edits narrative and polishing inside the story editor without a separate navigation entry', async () => {
  const root = mount()
  expect([...root.querySelectorAll('nav button')].some(b => b.textContent?.includes('叙事与润色'))).toBe(false)
  s.activePanel = 'chats'; await nextTick()
  get<HTMLButtonElement>(root, 'button[aria-label="故事设置 主故事"]').click(); await nextTick()
  const perspective = get<HTMLSelectElement>(root, 'select[aria-label="叙事人称"]')
  const polishing = get<HTMLSelectElement>(root, 'select[aria-label="润色等级"]')
  expect(perspective.value).toBe('third'); expect(polishing.value).toBe('basic')
  perspective.value = 'first'; perspective.dispatchEvent(new Event('change', { bubbles: true }))
  polishing.value = 'full'; polishing.dispatchEvent(new Event('change', { bubbles: true }))
  await nextTick()
  const mutate = vi.spyOn(s, 'mutate').mockResolvedValue(true)
  perspective.closest('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await nextTick()
  expect(mutate).toHaveBeenCalledWith('/chats/main', 'PATCH', { name: '主故事', narrative: { perspective: 'first' }, polishing: { level: 'full' } })
})

it('removes account, diagnostics, settings and the workspace strip', async () => {
 const root = mount()
 expect([...root.querySelectorAll('nav button')].map(b => b.textContent?.trim())).toEqual(['✦剧情', '▤世界书', '♙角色', '⑂故事', '◐主题'])
 expect(root.querySelector('.workspace-bar')).toBeNull()
 expect(root.textContent).not.toMatch(/连接与账号|诊断|实时连接|刷新|退出登录/)
 s.activePanel = 'chats'; await nextTick()
 const switchStory = vi.spyOn(s, 'switchTimeline').mockResolvedValue()
 get<HTMLButtonElement>(root, '[aria-label="切换故事 分支"]').click()
 expect(switchStory).toHaveBeenCalledWith('branch')
})
it.each([['off', '关闭润色'], ['basic', '基础润色'], ['full', '完全润色']] as const)('shows current story polishing %s alongside perspective and protection', async (level, label) => {
 const root = mount()
 s.state = { ...state, settings: { ...state.settings, narrative: { perspective: 'first' }, polishing: { level } }, protected_character: '角色·甲' }
 await nextTick()
 const toolbar = get(root, '.stream-toolbar')
 expect(toolbar.textContent).toContain('第一人称')
 expect(get(toolbar, '[data-testid="polishing-level"]').textContent).toBe(label)
 expect(toolbar.textContent).toContain('角色保护：角色·甲')
 s.state = { ...state, settings: { ...state.settings, polishing: { level: 'basic' } } }
 await nextTick()
 expect(toolbar.textContent).toContain('第三人称基础润色角色保护：无')
})

it('shows jump-to-latest only while reading away from the bottom', async () => {
 const root = mount(); s.events = [input]; await nextTick(); await nextTick()
 const scroll = get<HTMLElement>(root, '.history-scroll')
 Object.defineProperties(scroll, { scrollHeight: { configurable: true, value: 1600 }, clientHeight: { configurable: true, value: 400 } })
 expect(root.querySelector('.history-navigation')).toBeNull()
 scroll.scrollTop = 600; scroll.dispatchEvent(new Event('scroll')); await nextTick()
 expect(root.querySelector('.history-navigation')).not.toBeNull()
 scroll.scrollTop = 1200; scroll.dispatchEvent(new Event('scroll')); await nextTick()
 expect(root.querySelector('.history-navigation')).toBeNull()
 scroll.scrollTop = 600; scroll.dispatchEvent(new Event('scroll')); await nextTick()
 get<HTMLButtonElement>(root, '.history-navigation button').click(); await new Promise(r => setTimeout(r, 10)); await nextTick()
 expect(root.querySelector('.history-navigation')).toBeNull()
})

it('renders the selected character avatar next to the draft and falls back on image errors', async () => {
 const root = mount()
 const character = { metadata: { id: 'alice', name: 'Alice', enabled: true, avatar: 'avatar.png', body: '' }, content: '', path: '', sha256: 'hash', assets: [] }
 s.characters = [character]
 vi.mocked(fetch).mockImplementation((url, init) => String(url).includes('/media?character=alice') ? Promise.resolve(new Response(JSON.stringify({ data: [{ id: 'med_avatar', path: 'avatar.png', mime: 'image/png', name: 'avatar.png' }] }), { headers: { 'Content-Type': 'application/json' } })) : fakeAPI(String(url), init))
 s.inputIdentity = 'alice'; s.draft = '保留输入'; await new Promise(r => setTimeout(r, 10)); await nextTick()
 const img = get<HTMLImageElement>(root, '.composer-identity img')
 expect(img.getAttribute('src')).toBe('/api/v1/media/med_avatar')
 expect(img.alt).toBe('Alice的头像')
 expect(get(root, '.composer-writing textarea')).toBeTruthy()
 img.dispatchEvent(new Event('error')); await nextTick()
 expect(root.querySelector('.composer-identity img')).toBeNull()
 expect(get(root, '.identity-placeholder').textContent).toBe('A')
 s.inputIdentity = 'chat'; await nextTick()
 expect(get(root, '.composer-identity svg[role=img]').getAttribute('aria-label')).toBe('交流的头像')
 expect(s.draft).toBe('保留输入')
 expect(get<HTMLInputElement>(root, 'input[type=file]').classList.contains('sr-only')).toBe(true)
 const inputClick = vi.spyOn(get<HTMLInputElement>(root, 'input[type=file]'), 'click').mockImplementation(() => {})
 get<HTMLButtonElement>(root, '.attachment-button').click()
 expect(inputClick).toHaveBeenCalledOnce()
})

it('does not let a stale avatar response overwrite a new identity', async () => {
 const root = mount()
 s.characters = [{ metadata: { id: 'alice', name: 'Alice', enabled: true, avatar: 'avatar.png', body: '' }, content: '', path: '', sha256: 'hash', assets: [] }]
 let finish!: (r: Response) => void
 vi.mocked(fetch).mockImplementation((url, init) => String(url).includes('/media?character=alice') ? new Promise<Response>(resolve => { finish = resolve }) : fakeAPI(String(url), init))
 s.inputIdentity = 'alice'; await nextTick()
 s.inputIdentity = 'director'; await nextTick()
 finish(new Response(JSON.stringify({ data: [{ id: 'med_avatar', path: 'avatar.png', mime: 'image/png' }] })))
 await new Promise(r=>setTimeout(r,10)); await nextTick()
 expect(root.querySelector('.identity-avatar img')).toBeNull()
 expect(get(root, '.composer-identity svg[role=img]').getAttribute('aria-label')).toBe('导演的头像')
})

it('renders message portraits and opens the exact character card without changing identity', async () => {
 const root = mount()
 s.characters = ['alice','bob','carol','dave','eve'].map(id=>({metadata:{id,name:id,enabled:true,body:`${id} card`},content:'',path:'',sha256:id,assets:[]}))
 s.inputIdentity='director'
 s.events=[{...input,speaker:'alice'}, {...input,id:'story',type:'output',kind:'story',characters:['alice','bob','carol','dave','eve']}]
 await nextTick()
 expect(root.querySelector('[data-event-id="evt_first"] .message-portrait')).not.toBeNull()
 expect(root.querySelectorAll('.cast-portraits .character-portrait')).toHaveLength(4)
 get<HTMLButtonElement>(root,'[aria-label="下一组角色"]').click();await nextTick()
 expect(root.querySelector('.cast-portraits [aria-label="查看人物卡 eve"]')).not.toBeNull()
 get<HTMLButtonElement>(root,'.cast-portraits [aria-label="查看人物卡 eve"]').click();await nextTick();await nextTick()
 expect(s.activePanel).toBe('characters');expect(get<HTMLInputElement>(root,'[aria-label="角色名称"]').value).toBe('eve')
 expect(s.inputIdentity).toBe('director')
})
it('keeps worldbook categories collapsed by default with active category tools', async () => {
 const root=mount();s.worldbookCategories=['magic'];s.activePanel='worldbook';await nextTick()
 const toggle=get<HTMLButtonElement>(root,'[aria-label="展开分类 magic"]')
 expect(toggle.getAttribute('aria-expanded')).toBe('false')
 expect(get<HTMLButtonElement>(root,'[aria-label="启用分类 magic"]').disabled).toBe(false)
 expect(get<HTMLElement>(root,'.wb-entries').parentElement!.style.display).toBe('none')
 toggle.click();await nextTick();expect(toggle.getAttribute('aria-expanded')).toBe('true')
})
it('scopes media to the edited story tab, not the current story', async () => {
 const root=mount();s.activePanel='chats';await nextTick()
 get<HTMLButtonElement>(root,'[aria-label="故事设置 分支"]').click();await nextTick()
 get<HTMLButtonElement>(root,'.story-tabs button:last-child').click();await nextTick()
 expect(vi.mocked(fetch).mock.calls.some(([url])=>String(url).includes('/media?timeline=branch'))).toBe(true)
 expect(s.selectedTimeline).toBe('main');expect(root.querySelector('nav[aria-label="主导航"]')?.textContent).not.toContain('媒体')
})
it('only shows errors as dismissible transient snackbars', async () => {
 vi.useFakeTimers()
 try {
  const root=mount();s.error='保存失败';await nextTick();await nextTick()
  expect(root.querySelector('.alert,.notice')).toBeNull()
  expect(document.querySelector('.error-snackbar')?.textContent).toContain('保存失败')
  await vi.advanceTimersByTimeAsync(6100);await nextTick()
  expect(document.querySelector('.error-snackbar')).toBeNull()
 } finally { vi.useRealTimers() }
})

it('keeps an error snackbar interactive inside an open native media dialog', async () => {
 mount()
 const dialog = document.createElement('dialog'); dialog.setAttribute('open',''); document.body.appendChild(dialog)
 try {
  s.error='附件下载失败'; await nextTick(); await nextTick()
  expect(dialog.querySelector('.error-snackbar')).not.toBeNull()
  dialog.removeAttribute('open'); dialog.dispatchEvent(new Event('close')); await nextTick(); await nextTick()
  expect(dialog.querySelector('.error-snackbar')).toBeNull()
  get<HTMLButtonElement>(document.body,'[aria-label="关闭错误"]').click(); await nextTick()
  expect(s.error).toBe('')
 } finally { dialog.remove() }
})

it('opens story settings from its name without a separate edit button', async () => {
 const root=mount();s.activePanel='chats';await nextTick()
 expect([...root.querySelectorAll('.story-list button')].some(b=>b.textContent==='编辑')).toBe(false)
 expect(root.querySelector('[aria-label="切换故事 分支"]')).not.toBeNull()
 get<HTMLButtonElement>(root,'[aria-label="故事设置 主故事"]').click();await nextTick()
 expect(root.querySelector('[aria-label="故事名称"]')).not.toBeNull()
 expect(root.querySelector('.story-tabs')).not.toBeNull()
})

it('isolates message edits and branches in prototype-named stories', async () => {
  const root = mount()
  for (const name of ['__proto__', 'constructor']) {
    s.selectedTimeline = name
    s.events = [{ ...input, timeline: name }]
    await nextTick()
    expect(root.querySelector('[aria-label="创建故事分支"]')).toBeNull()
    expect(root.querySelector(`[aria-label="编辑正文 ${input.id}"]`)).toBeNull()
    get<HTMLButtonElement>(root, `[aria-label="编辑输入 ${input.id}"]`).click()
    await nextTick()
    expect(get<HTMLTextAreaElement>(root, `[aria-label="编辑正文 ${input.id}"]`).value).toBe(input.text)
    get<HTMLButtonElement>(root, `[aria-label="从此处分支 ${input.id}"]`).click()
    await nextTick()
    expect(root.querySelector('[aria-label="创建故事分支"]')).not.toBeNull()
  }
  expect(Object.prototype).not.toHaveProperty('id')
})
