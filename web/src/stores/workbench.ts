import { t } from '../lib/i18n'
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, APIError, upload } from '../lib/api'
import { mediaURL, previewKind } from '../lib/media'
import { getOwn, segment } from '../lib/types'
import type { MediaRef, MediaRecord, Character, Project, RPEvent, ServerStatus, Timeline, TimelineState, WorldEntry } from '../lib/types'

export const panels = [
  { id: 'writing', label: '剧情', mark: '✦' },
  { id: 'worldbook', label: '世界书', mark: '▤' },
  { id: 'characters', label: '角色', mark: '♙' },
  { id: 'chats', label: '故事', mark: '⑂' },
  { id: 'theme', label: '主题', mark: '◐' },
] as const
export type Panel = (typeof panels)[number]['id']
const PAGE_SIZE = 100

export const useWorkbenchStore = defineStore('workbench', () => {
  const authenticated = ref(false), booting = ref(true), syncing = ref(false), pending = ref(0)
  const error = ref('')
  const characterTarget = ref('')
  const avatars = new Map<string, { version: string; promise: Promise<string> }>()
  const activePanel = ref<Panel>('writing')
  const project = ref<Project>(), status = ref<ServerStatus>()
  const timelines = ref<Timeline[]>([]), characters = ref<Character[]>([]), worldbook = ref<WorldEntry[]>([])
  const worldbookCategories = ref<string[]>([])
  const selectedTimeline = ref(''), state = ref<TimelineState>(), events = ref<RPEvent[]>([])
  const hasMore = ref(false), loadingHistory = ref(false)
  const drafts = ref(new Map<string, string>()), identities = ref(new Map<string, string>())
  const attachments = ref(new Map<string, MediaRef[]>())
  const draftMedia = computed(() => attachments.value.get(selectedTimeline.value) ?? [])
  const scrollToLatest = ref(0)
  const timeline = computed(() => timelines.value.find((t) => t.id === selectedTimeline.value))
  const enabledCharacters = computed(() => characters.value.filter((c) => getOwn(state.value?.settings.characters, c.metadata.id) ?? c.metadata.enabled))
  const draft = computed({ get: () => drafts.value.get(selectedTimeline.value) ?? '', set: (v) => { drafts.value.set(selectedTimeline.value, v) } })
  const inputIdentity = computed({ get: () => identities.value.get(selectedTimeline.value) ?? 'director', set: (v) => { identities.value.set(selectedTimeline.value, v) } })
  const displayEvents = computed(() => events.value.filter((e) => e.type === 'input' || (e.type === 'output' && ['story', 'assistant'].includes(e.kind || ''))))
  let source: EventSource | undefined, timer: ReturnType<typeof setTimeout> | undefined
  let switching = false
  let generation = 0, syncAgain = false, disposed = false
  let syncTask: Promise<void> | undefined

  function report(err: unknown) {
    error.value = err instanceof APIError ? `${err.message} (${err.code})` : err instanceof Error ? err.message : String(err)
    if (err instanceof APIError && err.status === 401) {
      authenticated.value = false; stopStream(); avatars.clear(); generation++
      drafts.value.clear(); identities.value.clear(); attachments.value.clear(); events.value = []; state.value = undefined; project.value = undefined; status.value = undefined
      timelines.value = []; characters.value = []; worldbook.value = []; worldbookCategories.value = []
    }
  }
  function stopStream() { source?.close(); source = undefined; if (timer) clearTimeout(timer) }
  function scheduleSync() {
    if (disposed || !authenticated.value) return
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => { void sync() }, 120)
  }
  function openStream() {
    stopStream()
    source = new EventSource('/api/v1/events')
    source.onerror = scheduleSync
    for (const kind of ['resync.required', 'workspace.changed', 'timeline.event', 'server.status']) source.addEventListener(kind, scheduleSync)

  }
  async function bootstrap(token = '') {
    disposed = false; booting.value = true; error.value = ''
    try {
      if (token) { await api('/auth/session', 'POST', undefined, token); token = '' }
      const currentStatus = await api<ServerStatus>('/status')
      if (status.value && status.value.project !== currentStatus.project) {
        generation++; avatars.clear(); drafts.value.clear(); identities.value.clear(); attachments.value.clear(); events.value = []; loadingHistory.value = false; selectedTimeline.value = ''
      }
      status.value = currentStatus
      authenticated.value = true
      await sync()
      if (authenticated.value) openStream()
    } catch (err) { report(err) } finally { token = ''; booting.value = false }
  }
  async function readHistory(id: string, epoch: number) {
    const prior = events.value, oldest = prior[0]
    let loaded: RPEvent[] = [], more = true
    // Start at the tail; refresh only as far back as the reader has loaded.
    while (more) {
      const before = loaded[0]?.id
      const limit = before && oldest?.position && loaded[0]?.position ? Math.min(PAGE_SIZE, Math.max(1, loaded[0].position - oldest.position)) : PAGE_SIZE
      const chunk = await api<RPEvent[]>(`/chats/${segment(id)}/events?view=rp&tail=true&limit=${limit}${before ? `&before=${segment(before)}` : ''}`) ?? []
      if (epoch !== generation) return
      if (events.value !== prior) { syncAgain = true; return }
      if (before && chunk.some(e => e.id === before)) throw new Error(t('历史分页游标未前移'))
      loaded = [...chunk, ...loaded]; more = chunk.length === limit
      if (!oldest || loaded.some(e => e.id === oldest.id) || (oldest.position && loaded[0]?.position && loaded[0].position <= oldest.position)) break
    }
    const retained = oldest ? loaded.findIndex(e => e.id === oldest.id) : -1
    if (retained > 0) { loaded = loaded.slice(retained); more = true }
    events.value = loaded; hasMore.value = more && loaded[0]?.position !== 1

  }
  function sync(): Promise<void> {
    if (syncTask) { syncAgain = true; return syncTask }
    syncTask = (async () => {
      syncing.value = true
      do { syncAgain = false; await refresh() }
      while (syncAgain && authenticated.value && !disposed)
    })().finally(() => { syncing.value = false; syncTask = undefined })
    return syncTask
  }
  async function refresh() {
    if (!authenticated.value || disposed) return
    let epoch = generation
    try {
      const [p, ts, cs, ws, categories] = await Promise.all([
        api<Project>('/project'), api<Timeline[]>('/chats'), api<Character[]>('/characters'),
        api<WorldEntry[]>('/worldbook'),
        api<string[]>('/worldbook/categories'),
      ])
      if (epoch !== generation) return
      worldbookCategories.value = categories ?? []; project.value = p; timelines.value = ts ?? []; characters.value = cs ?? []; worldbook.value = ws ?? []
      if (!timelines.value.some((t) => t.id === selectedTimeline.value)) {
        epoch = ++generation
        state.value = undefined; hasMore.value = false
        selectedTimeline.value = timelines.value.find((t) => t.id === p.last_chat)?.id ?? timelines.value.find((t) => !t.archived)?.id ?? timelines.value[0]?.id ?? ''
        events.value = []; loadingHistory.value = false
      }
      const id = selectedTimeline.value
      if (id) {
        const current = await api<TimelineState>(`/chats/${segment(id)}/state`)
        if (epoch !== generation) return
        state.value = current
        await readHistory(id, epoch)
        if (epoch !== generation) return
        if (!identities.value.has(id)) identities.value.set(id, current.last_speaker || 'director')
        if (!['chat', 'director', ...enabledCharacters.value.map((c) => c.metadata.id)].includes(inputIdentity.value)) inputIdentity.value = 'director'
      }
    } catch (err) { if (epoch === generation) report(err) }
  }
  async function switchTimeline(id: string) {
    if (id === selectedTimeline.value || switching) return
    switching = true; pending.value++; error.value = ''
    try {
      await api('/project', 'PATCH', { last_chat: id })
      generation++; identities.value.delete(id); selectedTimeline.value = id; events.value = []; state.value = undefined
      loadingHistory.value = false; hasMore.value = false
      await sync()
    } catch (err) { report(err) }
    finally { switching = false; pending.value-- }
  }
  async function loadMore() {
    if (loadingHistory.value || !hasMore.value || !events.value.length) return
    loadingHistory.value = true
    const epoch = generation, id = selectedTimeline.value, prior = events.value
    try {
      const chunk = await api<RPEvent[]>(`/chats/${segment(id)}/events?view=rp&tail=true&limit=${PAGE_SIZE}&before=${segment(prior[0]!.id)}`) ?? []
      if (epoch !== generation || events.value !== prior) return
      const ids = new Set(prior.map(e => e.id))
      events.value = [...chunk.filter(e => !ids.has(e.id)), ...prior]
      hasMore.value = chunk.length === PAGE_SIZE
    } catch (err) {
      if (epoch === generation) {
        if (err instanceof APIError && err.status === 404) scheduleSync()
        else report(err)
      }
    } finally { if (epoch === generation) loadingHistory.value = false }
  }
  async function latest() {
    const epoch = generation
    if (syncTask) await syncTask
    if (epoch === generation) scrollToLatest.value++
  }
  async function mutate(path: string, method: string, body: unknown): Promise<boolean> {
    pending.value++; error.value = ''
    try {
      const result = await api<{ id?: string }>(path, method, body)
      if (method === 'PATCH' && /^\/chats\/[^/]+$/.test(path) && result?.id) {
        const old = decodeURIComponent(path.split('/')[2]!), name = result.id
        if (old !== name) {
          if (identities.value.has(old)) { identities.value.set(name, identities.value.get(old)!); identities.value.delete(old) }
          if (drafts.value.has(old)) { drafts.value.set(name, drafts.value.get(old)!); drafts.value.delete(old) }
          if (attachments.value.has(old)) {
            attachments.value.set(name, attachments.value.get(old)!)
            attachments.value.delete(old)
          }
          if (selectedTimeline.value === old) { generation++; selectedTimeline.value = name; events.value = []; state.value = undefined; loadingHistory.value = false; hasMore.value = false }
        }
      }
      if (/^\/chats\/[^/]+\/events\/[^/]+$/.test(path)) {
        generation++; loadingHistory.value = false
      }
      await sync(); return true
    }
    catch (err) {
      report(err)
      if (err instanceof APIError && err.code === 'input_saved_delivery_failed') { await sync(); return true }
      return false
    }
    finally { pending.value-- }
  }
  async function uploadAttachment(file: File) {
    const id = selectedTimeline.value
    if (!id || pending.value) return
    pending.value++; error.value = ''
    try {
      const media = await upload<MediaRecord>('/media', file, { timeline: id })
      if (!authenticated.value || disposed) return false
      // Server records have extra metadata that event schema deliberately rejects.
      const item = { id: media.id, path: media.path, mime: media.mime, name: media.name }
      attachments.value.set(id, [...(attachments.value.get(id) ?? []), item])
      return true
    } catch (e) { report(e); return false } finally { pending.value-- }
  }
  function attachExisting(media: MediaRef, id = selectedTimeline.value) {
    const draft = attachments.value.get(id) ?? []
    if (!draft.some((item) => item.id === media.id)) attachments.value.set(id, [...draft, { id: media.id, path: media.path, mime: media.mime, name: media.name }])
  }
  function removeAttachment(id: string, story = selectedTimeline.value) { attachments.value.set(story, (attachments.value.get(story) ?? []).filter((m) => m.id !== id)) }
  async function send() {
    if ((!draft.value.trim() && !draftMedia.value.length) || pending.value || !selectedTimeline.value) return
    const id = selectedTimeline.value, original = draft.value, media = [...draftMedia.value]
    if (await mutate(`/chats/${segment(id)}/inputs`, 'POST', { speaker: inputIdentity.value, text: original, media })) {
      if (drafts.value.get(id) === original) drafts.value.set(id, '')
      attachments.value.set(id, (attachments.value.get(id) ?? []).filter((m) => !media.some((sent) => sent.id === m.id)))
      if (id === selectedTimeline.value) await latest()
    }
  }
  async function branchFrom(eventID: string, name: string) {
    if (pending.value || !selectedTimeline.value) return false
    const source = selectedTimeline.value
    pending.value++; error.value = ''
    try {
      const position = events.value.find(e => e.id === eventID)?.position
      const chat = await api<Timeline>(`/chats/${segment(source)}/branch`, 'POST', { ...(position ? { position } : { event_id: eventID }), name })
      if (selectedTimeline.value === source) await switchTimeline(chat.id)
      else await sync()
      return true
    } catch (err) { report(err); return false } finally { pending.value-- }
  }
  function openCharacter(id: string) {
    if (!characters.value.some(c => c.metadata.id === id)) return
    characterTarget.value = id; activePanel.value = 'characters'
  }
  function avatarFor(id: string): Promise<string> {
    const card = characters.value.find(c => c.metadata.id === id)
    if (!card?.metadata.avatar) return Promise.resolve('')
    const version = `${card.sha256}/${card.metadata.avatar}`
    const cached = avatars.get(id)
    if (cached?.version === version) return cached.promise
    const promise = api<MediaRecord[]>(`/media?character=${segment(id)}`).then(records => {
      const item = records?.find(m => m.path === card.metadata.avatar && previewKind(m.mime) === 'image')
      return item ? mediaURL(item) : ''
    }).catch(err => {
      if (avatars.get(id)?.promise === promise) avatars.delete(id)
      if (err instanceof APIError && err.status === 401) report(err)
      return ''
    })
    avatars.set(id, { version, promise }); return promise
  }
  function dispose() { disposed = true; stopStream(); generation++ }
  return { authenticated, booting, syncing, pending, error, characterTarget, avatarFor, openCharacter, activePanel, displayEvents, project,
    timelines, characters, worldbook, worldbookCategories, selectedTimeline, timeline, state, events, hasMore, loadingHistory, draft, inputIdentity,
    draftMedia, uploadAttachment, attachExisting, removeAttachment, enabledCharacters, scrollToLatest, bootstrap, sync, switchTimeline, loadMore, latest, mutate, send, branchFrom, report, dispose }
})
