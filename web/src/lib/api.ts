import { t } from "./i18n"
export class APIError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message) }
}
export function consumeFragment(): string {
  const fragment = location.hash.slice(1)
  const params = new URLSearchParams(fragment)
  const token = params.get('token') ?? ''
  if (token || params.has('token')) history.replaceState(null, '', location.pathname + location.search)
  return token
}
export async function api<T>(path: string, method = 'GET', body?: unknown, bearer?: string): Promise<T> {
  const serialized = body === undefined ? undefined : JSON.stringify(body)
  const mutation = method !== 'GET'
  const headers: Record<string, string> = {}
  if (mutation) {
    headers['Content-Type'] = 'application/json'
  }
  if (bearer) headers.Authorization = `Bearer ${bearer}`
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 20000)
  try {
    const response = await fetch(`/api/v1${path}`, { method, headers, body: serialized, credentials: 'same-origin', signal: controller.signal, cache: 'no-store' })
    const envelope = await response.json()
    if (!response.ok) {
      throw new APIError(response.status, envelope.error?.code ?? 'http_error', envelope.error?.message ?? `HTTP ${response.status}`)
    }
    if (!('data' in envelope)) throw new Error(t("服务器响应缺少 data 字段"))
    return envelope.data as T
  } catch (error) {
    if (error instanceof APIError) throw error
    throw new APIError(0, 'network_error', t("连接中断。请先检查已保存的内容，再决定是否重新提交。"))
  } finally { clearTimeout(timeout) }
}

export const MEDIA_LIMIT = 32 * 1024 * 1024
export const ARCHIVE_LIMIT = 64 * 1024 * 1024
export function checkFile(file: File, limit: number) {
  if (!file.size) throw new APIError(422, 'empty_file', t("不能上传空文件。"))
  if (file.size > limit) throw new APIError(413, 'file_too_large', t("文件超过 {0} MiB 上限。请选择较小文件。", [limit / 1024 / 1024]))
}
export async function upload<T>(path: string, file: File, fields: Record<string, string> = {}, limit = MEDIA_LIMIT): Promise<T> {
  checkFile(file, limit)
  const body = new FormData()
  body.append('file', file)
  for (const [name, value] of Object.entries(fields)) if (value) body.append(name, value)
  const controller = new AbortController(), timeout = setTimeout(() => controller.abort(), 120000)
  try {
    const response = await fetch(`/api/v1${path}`, { method: 'POST', body, credentials: 'same-origin', cache: 'no-store', signal: controller.signal })
    const envelope = await response.json()
    if (!response.ok) {
      throw new APIError(response.status, envelope.error?.code ?? 'upload_error', envelope.error?.message ?? t("上传失败 HTTP {0}", [response.status]))
    }
    if (!('data' in envelope)) throw new Error('invalid envelope')
    return envelope.data as T
  } catch (error) {
    if (error instanceof APIError) throw error
    throw new APIError(0, 'network_error', t("上传中断。请先检查媒体列表，再决定是否重新上传。"))
  } finally { clearTimeout(timeout) }
}

export async function download(path: string, filename: string, method = 'GET', expected = 'application/zip'): Promise<void> {
  const controller = new AbortController(), timeout = setTimeout(() => controller.abort(), 120000)
  try {
    const response = await fetch(`/api/v1${path}`, { method, credentials: 'same-origin', cache: 'no-store', signal: controller.signal, ...(method === 'POST' ? {headers: {'Content-Type': 'application/json'}, body: '{}'} : {}) })
    if (!response.ok) {
      const envelope = await response.json().catch(() => ({}))
      throw new APIError(response.status, envelope.error?.code ?? 'download_error', envelope.error?.message ?? t("下载失败 HTTP {0}", [response.status]))
    }
    if (expected && response.headers.get('Content-Type')?.split(';')[0] !== expected) throw new APIError(0, 'invalid_download', t("服务器未返回预期的文件类型，未保存文件。"))
    const blob = await response.blob()
    if (!blob.size) throw new APIError(0, 'empty_download', t("服务器返回了空文件。"))
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a'); link.href = url; link.download = filename; link.hidden = true
    document.body.append(link); link.click(); link.remove()
    // Give the browser time to consume the URL without retaining it indefinitely.
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  } catch (error) {
    if (error instanceof APIError) throw error
    throw new APIError(0, 'download_error', t("下载中断或浏览器无法保存文件，请重试。"))
  } finally { clearTimeout(timeout) }
}
