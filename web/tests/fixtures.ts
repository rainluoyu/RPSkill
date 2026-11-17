import type { Settings, TimelineState, Project, RPEvent, ServerStatus, Timeline } from '../src/lib/types'
export const settings: Settings = { narrative: { perspective: 'third' }, polishing: { level: 'basic' }, worldbook: { scan_recent: 0, categories: {}, entries: {} }, characters: {} }
export const state: TimelineState = { settings, characters: {} }
export const project: Project = { name: '测试工作区', version: 3, last_chat: 'main' }
export const timelines: Timeline[] = [{ ...settings, id: 'main', name: '主故事', archived: false }, { ...settings, id: 'branch', name: '分支', archived: false }]
export const status: ServerStatus = { project: '/workspace', callback: 'codex', version: 'test', phase: 'phase2' }
export const input: RPEvent = { id: 'evt_first', type: 'input', time: '2026-11-12T01:00:00Z', timeline: 'main', speaker: 'director', text: '下雨了' }
export function response(data: unknown, status = 200) { return new Response(JSON.stringify({ data }), { status, headers: { 'Content-Type': 'application/json' } }) }
export function fakeAPI(url: string, init?: RequestInit) {
  if (init?.method !== 'GET') return Promise.resolve(response({}))
  const path = url.split('?')[0]
  if (path === '/api/v1/status') return Promise.resolve(response(status))
  if (path === '/api/v1/project') return Promise.resolve(response(project))
  if (path === '/api/v1/chats') return Promise.resolve(response(timelines))
  if (path?.endsWith('/state')) return Promise.resolve(response(state))
  return Promise.resolve(response([]))
}
export class TestEventSource {
  static instances: TestEventSource[] = []
  onopen?: () => void
  onerror?: () => void
  closed = false
  listeners = new Map<string, Array<(event: MessageEvent) => void>>()
  constructor(public url: string) { TestEventSource.instances.push(this) }
  addEventListener(type: string, listener: (event: MessageEvent) => void) { this.listeners.set(type, [...this.listeners.get(type) ?? [], listener]) }
  emit(type: string, data: unknown) { for (const listener of this.listeners.get(type) ?? []) listener(new MessageEvent(type, { data: JSON.stringify(data) })) }
  close() { this.closed = true }
}
