export type Perspective = 'first' | 'third'
export type Polishing = 'off' | 'basic' | 'full'
export interface WorldSettings { scan_recent: number; categories: Record<string, boolean>; entries: Record<string, boolean>; }
export interface Settings { narrative: { perspective: Perspective }; polishing: { level: Polishing }; worldbook: WorldSettings; characters: Record<string, boolean>; provider?: string }
export interface SettingsPatch { narrative?: Settings['narrative']; polishing?: Settings['polishing']; worldbook?: Partial<WorldSettings>; characters?: Record<string, boolean>; provider?: string }
export interface Timeline extends SettingsPatch { id: string; name: string; archived?: boolean }
export interface Project { last_chat: string; provider?: string; chats?: Record<string,{name:string;archived?:boolean}>; name: string; version: number }
export interface TimelineState { settings: Settings; last_speaker?: string; protected_character?: string; characters: Record<string, string> }
export interface Character { path: string; content: string; sha256: string; metadata: { id: string; name: string; enabled: boolean; avatar?: string; body: string }; assets: Array<{ path: string }> }
export interface WorldEntry { category: string; slug: string; path: string; content: string; sha256: string; metadata: { id: string; name: string; enabled: boolean; activation: 'always' | 'keyword'; keys: string[]; priority: number; body: string } }
export interface MediaRef { id: string; path: string; mime: string; name: string }
export interface MediaRecord extends MediaRef { size: number; sha256: string; timeline?: string; character?: string; expression?: string; preview: boolean }
export interface RPEvent { position?: number; id: string; type: string; time: string; timeline: string; speaker?: string; text?: string; kind?: string; media?: MediaRef[]; characters?: string[] }
export interface ServerStatus { project: string; callback: string; version: string; phase: string }
export const segment = (id: string) => encodeURIComponent(id)
export function getOwn<T>(record: Record<string, T> | undefined, key: string): T | undefined {
  // Read first so Vue tracks a missing key; hasOwn alone is not a reactive get.
  const value = record?.[key]
  return record && Object.hasOwn(record, key) ? value : undefined
}
