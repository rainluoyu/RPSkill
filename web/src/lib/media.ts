import type { MediaRef } from './types'
// Never use a path, MIME-derived URL, remote URL or token from story text.
export function mediaURL(media: MediaRef, download = false): string {
  if (!/^med_[a-z0-9_-]{1,60}$/.test(media.id)) return ''
  return `/api/v1/media/${encodeURIComponent(media.id)}${download ? '?download=1' : ''}`
}
export function previewKind(mime: string): 'image' | 'audio' | 'video' | undefined {
  if (['image/png', 'image/jpeg', 'image/gif', 'image/webp'].includes(mime)) return 'image'
  if (['audio/mpeg', 'audio/wave', 'audio/wav', 'audio/ogg', 'audio/flac', 'audio/aiff'].includes(mime)) return 'audio'
  if (['video/ogg', 'video/mp4', 'video/webm'].includes(mime)) return 'video'
}
export function reference(m: MediaRef): MediaRef { return { id: m.id, path: m.path, mime: m.mime, name: m.name } }
