import { setLocale } from '../src/lib/i18n'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useWorkbenchStore } from '../src/stores/workbench'
import { fakeAPI, input, response, settings, state, TestEventSource } from './fixtures'

let store: ReturnType<typeof useWorkbenchStore>
beforeEach(() => { setLocale('zh-CN'); setActivePinia(createPinia()); store = useWorkbenchStore(); vi.stubGlobal('fetch', vi.fn(fakeAPI)); TestEventSource.instances = []; vi.stubGlobal('EventSource', TestEventSource) })
afterEach(() => { store.dispose(); vi.unstubAllGlobals() })
describe('P3 authenticated workbench', () => {
  it('bootstraps using cookie requests and opens a same-origin event stream', async () => {
    await store.bootstrap('private-token')
    expect(store.authenticated).toBe(true)
    expect(store.project?.name).toBe('测试工作区')
    expect(store.selectedTimeline).toBe('main')
    expect(TestEventSource.instances[0]?.url).toBe('/api/v1/events')
    const calls = vi.mocked(fetch).mock.calls
    expect((calls[0]?.[1]?.headers as Record<string, string>).Authorization).toBe('Bearer private-token')
    for (const call of calls.slice(1)) expect(call[1]?.headers).not.toHaveProperty('Authorization')
    expect(JSON.stringify(store.$state)).not.toContain('private-token')
  })
  it('retains drafts but restores the last sent identity when opening each story', async () => {
    await store.bootstrap()
    store.draft = 'main draft'; store.inputIdentity = 'chat'
    await store.switchTimeline('branch')
    expect(store.draft).toBe(''); expect(store.inputIdentity).toBe('director')
    store.draft = 'branch draft'
    await store.switchTimeline('main')
    expect(store.draft).toBe('main draft'); expect(store.inputIdentity).toBe('director')
  })
  it('does not change the protected character merely by selecting an identity', async () => {
    await store.bootstrap()
    store.state = { ...state, protected_character: 'alice' }
    store.inputIdentity = 'chat'
    expect(store.state.protected_character).toBe('alice')
    expect(vi.mocked(fetch).mock.calls.filter(([, init]) => init?.method !== 'GET')).toHaveLength(0)
  })
  it('ignores agent diagnostic text as a formal source', async () => {
    await store.bootstrap()
    TestEventSource.instances[0]!.emit('agent.event', { text: 'not a story', kind: 'story' })
    expect(store.events).toEqual([])
  })
  it('surfaces validation errors and retains unsent input', async () => {
    await store.bootstrap(); store.draft = 'keep me'
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify({ error: { code: 'validation_error', message: '拒绝' } }), { status: 422 }))))
    await store.send(); expect(store.error).toContain('validation_error'); expect(store.draft).toBe('keep me'); expect(store.pending).toBe(0)
  })
  it('drops private fetched data and closes the stream after unauthorized resync', async () => {
    await store.bootstrap()
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify({ error: { code: 'unauthorized', message: '登录已过期' } }), { status: 401 }))))
    await store.sync(); expect(store.authenticated).toBe(false); expect(store.events).toEqual([]); expect(store.project).toBeUndefined(); expect(TestEventSource.instances[0]?.closed).toBe(true)
  })
  it('handles valid IDs which coincide with Object prototype names', async () => {
    await store.bootstrap()
    store.selectedTimeline = 'constructor'
    expect(store.draft).toBe(''); expect(store.inputIdentity).toBe('director')
    store.characters = [{ metadata: { id: 'constructor', name: 'Name', enabled: false, body: '' }, content: '', path: '', sha256: '', assets: [] }]
    expect(store.enabledCharacters).toEqual([])
    store.draft = 'safe draft'; expect(store.draft).toBe('safe draft')
  })
  it('does not apply an old history response to a newly selected timeline', async () => {
    await store.bootstrap()
    let finish: ((value: Response) => void) | undefined
    vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => url.includes('/chats/main/events?') ? new Promise<Response>((resolve) => { finish = resolve }) : url.includes('/chats/branch/events?') ? Promise.resolve(response([{ ...input, id: 'evt_branch', timeline: 'branch', type: 'input' }])) : fakeAPI(url, init)))
    const refresh = store.sync()
    await new Promise((resolve) => setTimeout(resolve, 10))
    const switching = store.switchTimeline('branch')
    finish!(response([input]))
    await Promise.all([refresh, switching])
    expect(store.selectedTimeline).toBe('branch'); expect(store.events.map((e) => e.id)).toEqual(['evt_branch'])
  })
  it('derives enabled characters from explicit false overrides', async () => {
    await store.bootstrap()
    store.characters = [{ metadata: { id: 'alice', name: 'Alice', enabled: true, body: '' }, content: '', path: '', sha256: '', assets: [] }]
    store.state = { ...state, settings: { ...settings, characters: { alice: false } } }
    expect(store.enabledCharacters).toEqual([])
  })
})






it('reuses fresh summary states instead of refetching one full request per historical input', async () => {
 await store.bootstrap()
 const history=Array.from({length:100},(_,n)=>({...input,id:`evt_${n}`}))
 const summaries=history.map(e=>({request_id:e.id,timeline:'main',provider:'codex',harness_state:'completed',delivery_state:'complete',outputs:null,updated_at:''}))
 const fetcher=vi.fn((url:string,init?:RequestInit)=>url.endsWith('/requests') ? Promise.resolve(response(summaries)) : url.includes('/events?') ? Promise.resolve(response(history)) : fakeAPI(url,init))
 vi.stubGlobal('fetch',fetcher);await store.sync()
 expect(store.events).toHaveLength(100);expect(fetcher.mock.calls.filter(([url])=>url.includes('/requests/'))).toHaveLength(0)
 // Even when a completed request ages out of the bounded summary, its final
 // status remains immutable. Mutable/missing delivery states are not cached.
 fetcher.mockClear();await store.sync();expect(fetcher.mock.calls.filter(([url])=>url.includes('/requests/'))).toHaveLength(0)
})

it('does not resurrect deleted tail outputs from the previous history snapshot', async () => {
 await store.bootstrap()
 store.events=[input,{...input,id:'deleted',type:'output',kind:'story'}]; store.hasMore=false
 vi.stubGlobal('fetch',vi.fn((url:string,init?:RequestInit)=>url.includes('/events?')?Promise.resolve(response([input])):fakeAPI(url,init)))
 await store.sync(); expect(store.events.map(e=>e.id)).toEqual([input.id])
})

it('creates one independent branch and switches without sending a model input', async () => {
 await store.bootstrap(); store.draft = 'source draft'
 const fetcher = vi.fn((url: string, init?: RequestInit) => url.endsWith('/main/branch') ? Promise.resolve(response({ id: 'branch', name: 'Branch', archived: false })) : fakeAPI(url, init))
 vi.stubGlobal('fetch', fetcher)
 expect(await store.branchFrom(input.id, 'Branch')).toBe(true)
 expect(store.selectedTimeline).toBe('branch'); expect(store.draft).toBe('')
 const writes = fetcher.mock.calls.filter(([, init]) => init?.method !== 'GET')
 expect(writes).toHaveLength(2)
 expect(JSON.parse(writes[1]![1]!.body as string)).toEqual({ last_chat: 'branch' })
 expect(JSON.parse(writes[0]![1]!.body as string)).toEqual({ event_id: input.id, name: 'Branch' })
 await store.switchTimeline('main'); expect(store.draft).toBe('source draft')
})
it('preserves the current story on branch failure and ignores repeated submissions while pending', async () => {
 await store.bootstrap()
 vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify({ error: { code: 'conflict', message: '历史已改变' } }), { status: 409 }))))
 expect(await store.branchFrom(input.id, 'Branch')).toBe(false)
 expect(store.selectedTimeline).toBe('main'); expect(store.error).toContain('历史已改变')
 store.pending = 1
 expect(await store.branchFrom(input.id, 'Branch')).toBe(false)
 expect(fetch).toHaveBeenCalledTimes(1)
})

describe('story file settings', () => {
  it('restores last_chat and persists a successful switch', async () => {
    vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => url === '/api/v1/project' && init?.method === 'GET' ? Promise.resolve(response({ name: 'Test', version: 3, last_chat: 'branch' })) : fakeAPI(url, init)))
    await store.bootstrap()
    expect(store.selectedTimeline).toBe('branch')
    await store.switchTimeline('main')
    expect(store.selectedTimeline).toBe('main')
    expect(vi.mocked(fetch).mock.calls.some(([url, init]) => url === '/api/v1/project' && init?.method === 'PATCH' && JSON.parse(String(init.body)).last_chat === 'main')).toBe(true)
  })
  it('keeps the current story when persisting selection fails', async () => {
    await store.bootstrap()
    store.draft = 'unsaved prompt'
    vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => init?.method === 'PATCH' ? Promise.resolve(response({ error: 'no' }, 500)) : fakeAPI(url, init)))
    await store.switchTimeline('branch')
    expect(store.selectedTimeline).toBe('main')
    expect(store.draft).toBe('unsaved prompt')
    expect(store.pending).toBe(0)
  })
})

it('does not query removed provider or diagnostic panels during synchronization', async () => {
 await store.bootstrap(); await store.sync()
 expect(vi.mocked(fetch).mock.calls.some(([url]) => String(url).includes('/providers') || String(url).includes('view=diagnostic'))).toBe(false)
})

function historyAPI(history: typeof input[]) {
 return vi.fn((url: string, init?: RequestInit) => {
  if (!url.includes('/events?')) return fakeAPI(url, init)
  const query = new URL(url, 'http://localhost').searchParams
  const before = query.get('before'), index = before ? history.findIndex(e => e.id === before) : history.length
  return Promise.resolve(response(history.slice(0,index).slice(-Number(query.get('limit')))))
 })
}
const messages = (count: number) => Array.from({length:count},(_,n)=>({...input, id:`m${n+1}`, position:n+1, type:'output', kind:'story'}))
it('loads the newest batch first and prepends older messages without duplicates', async () => {
 const history=messages(230), fetcher=historyAPI(history); vi.stubGlobal('fetch',fetcher)
 await store.bootstrap()
 expect(store.events).toEqual(history.slice(130));expect(store.hasMore).toBe(true)
 expect(fetcher.mock.calls.filter(([url])=>url.includes('/events?'))).toHaveLength(1)
 await store.loadMore();expect(store.events).toEqual(history.slice(30))
 await store.loadMore();expect(store.events).toEqual(history);expect(store.hasMore).toBe(false)
 const count=fetcher.mock.calls.length;await store.latest();expect(fetcher.mock.calls).toHaveLength(count);expect(store.scrollToLatest).toBe(1)
})
it('retains the loaded reading range while appending new output, and refreshes truncation', async () => {
 const history=messages(230);vi.stubGlobal('fetch',historyAPI(history));await store.bootstrap();await store.loadMore()
 history.push(...messages(231).slice(230));await store.sync()
 expect(store.events.at(-1)?.id).toBe('m231');expect(store.events[0]?.id).toBe('m31');expect(store.events).toHaveLength(201)
 history.splice(100);await store.sync();expect(store.events).toEqual(history.slice(30))
 history.splice(0);await store.sync();expect(store.events).toEqual([]);expect(store.hasMore).toBe(false)
})
it('discards an older-page response when the story changes', async () => {
 const history=messages(230);vi.stubGlobal('fetch',historyAPI(history));await store.bootstrap()
 let finish!: (r: Response)=>void
 vi.stubGlobal('fetch',vi.fn((url:string,init?:RequestInit)=>url.includes('before=')?new Promise<Response>(resolve=>{finish=resolve}):fakeAPI(url,init)))
 const loading=store.loadMore();await store.switchTimeline('branch');finish(response(history.slice(30,130)));await loading
 expect(store.selectedTimeline).toBe('branch');expect(store.events).toEqual([])
})
it('does not overwrite prepended history when a tail refresh completes concurrently', async () => {
 const history=messages(230), handler=historyAPI(history);vi.stubGlobal('fetch',handler);await store.bootstrap()
 let finish!: (r:Response)=>void, delayed=false
 vi.stubGlobal('fetch',vi.fn((url:string,init?:RequestInit)=>{
  if(url.includes('/events?')&&!url.includes('before=')&&!delayed){delayed=true;return new Promise<Response>(resolve=>{finish=resolve})}
  return handler(url,init)
 }))
 const refresh=store.sync();await new Promise(r=>setTimeout(r,10));await store.loadMore();finish(response(history.slice(130)));await refresh
 expect(store.events.some(e=>e.id==='m31')).toBe(true);expect(new Set(store.events.map(e=>e.id)).size).toBe(store.events.length)
})

it('keeps drafts identity and media attached when the story folder is renamed', async () => {
 await store.bootstrap();store.draft='unsent';store.inputIdentity='chat'
 store.attachExisting({id:'med_x',path:'media/x.png',mime:'image/png',name:'x.png'})
 vi.mocked(fetch).mockImplementation((url,init)=>{
  if(init?.method==='PATCH')return Promise.resolve(response({id:'示例故事',name:'示例故事'}))
  if(String(url)==='/api/v1/project')return Promise.resolve(response({name:'Workspace',version:3,last_chat:'示例故事'}))
  if(String(url)==='/api/v1/chats')return Promise.resolve(response([{id:'示例故事',name:'示例故事',...settings}]))
  return fakeAPI(String(url),init)
 })
 expect(await store.mutate('/chats/main','PATCH',{name:'示例故事'})).toBe(true)
 expect(store.selectedTimeline).toBe('示例故事');expect(store.draft).toBe('unsent');expect(store.inputIdentity).toBe('chat')
 expect(store.draftMedia[0]?.path).toBe('media/x.png');expect(store.error).toBe('')
})

it('keeps prototype-named story drafts, identities and media as own data', () => {
  const media = { id: 'media_test', path: 'media/a.png', mime: 'image/png', name: 'a.png' }
  for (const name of ['__proto__', 'constructor', 'toString']) {
    store.selectedTimeline = name
    expect(store.draft).toBe(''); expect(store.inputIdentity).toBe('director'); expect(store.draftMedia).toEqual([])
    store.draft = name; store.inputIdentity = 'chat'; store.attachExisting(media)
  }
  for (const name of ['__proto__', 'constructor', 'toString']) {
    store.selectedTimeline = name
    expect(store.draft).toBe(name); expect(store.inputIdentity).toBe('chat'); expect(store.draftMedia[0]?.id).toBe(media.id)
  }
  expect(Object.getPrototypeOf({})).toBe(Object.prototype)
  expect(Object.prototype).not.toHaveProperty('id')
})

it('never queries execution state and clears an input that was saved before inbox failure', async () => {
 await store.bootstrap()
 expect(vi.mocked(fetch).mock.calls.some(([url])=>String(url).includes('/requests'))).toBe(false)
 store.draft='already saved'
 vi.mocked(fetch).mockImplementation((url,init)=> {
  if(String(url).endsWith('/inputs'))return Promise.resolve(new Response(JSON.stringify({error:{code:'input_saved_delivery_failed',message:'Input saved; owner unavailable'}}),{status:502}))
  return fakeAPI(String(url),init)
 })
 await store.send()
 expect(store.draft).toBe('')
 expect(store.error).toContain('input_saved_delivery_failed')
 expect(vi.mocked(fetch).mock.calls.filter(([url])=>String(url).endsWith('/inputs'))).toHaveLength(1)
})

it.each(['alice', 'director', 'chat'])('restores the last input identity %s even when the latest page contains only outputs', async (speaker) => {
 vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => {
  if (url.endsWith('/state')) return Promise.resolve(response({ ...state, last_speaker: speaker }))
  if (url === '/api/v1/characters') return Promise.resolve(response([{ metadata: { id: 'alice', enabled: true } }]))
  if (url.includes('/events?')) return Promise.resolve(response(messages(100)))
  return fakeAPI(url, init)
 }))
 await store.bootstrap()
 expect(store.inputIdentity).toBe(speaker)
 store.inputIdentity = speaker === 'chat' ? 'director' : 'chat'
 const selected = store.inputIdentity
 await store.sync()
 expect(store.inputIdentity).toBe(selected)
 await store.switchTimeline('branch'); await store.switchTimeline('main')
 expect(store.inputIdentity).toBe(speaker)
})

it('falls back to director when the last input character is no longer enabled', async () => {
 vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => url.endsWith('/state') ? Promise.resolve(response({ ...state, last_speaker: 'removed' })) : fakeAPI(url, init)))
 await store.bootstrap()
 expect(store.inputIdentity).toBe('director')
})
