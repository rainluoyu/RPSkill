import type { IncomingMessage, ServerResponse } from 'node:http'
import type { Plugin, ProxyOptions, ViteDevServer, PreviewServer } from 'vite'

const loopback = (hostname: string) => ['127.0.0.1', 'localhost', '[::1]'].includes(hostname)
export function validateOrigin(host: string | undefined, origin: string | undefined, method: string, cookie: string | undefined, fetchSite: string | undefined, bearer: string | undefined): boolean {
  try {
    const local = new URL(`http://${host}`)
    if (!host || !loopback(local.hostname) || local.host !== host || fetchSite === 'cross-site') return false
    if (origin !== undefined && origin !== local.origin) return false
    if (!['GET', 'HEAD', 'OPTIONS'].includes(method) && cookie?.split(';').some(value => value.trim().startsWith('rps_session_')) && !bearer && !origin) return false
    return true
  } catch { return false }
}
// Node pipe() does not propagate an aborted readable to its destination.
// Without this bridge a broken upstream SSE can leave EventSource falsely open.
export function bridgeEventStream(incoming: IncomingMessage, outgoing: ServerResponse) {
  const disconnect = () => { if (!outgoing.destroyed && !outgoing.writableEnded) outgoing.destroy() }
  incoming.once('aborted', disconnect)
  incoming.on('error', disconnect)
  incoming.once('close', () => { if (!incoming.complete) disconnect() })
  outgoing.once('close', () => { if (!incoming.destroyed) incoming.destroy() })
}
export function devProxy(target: string): { guard: Plugin; proxy: ProxyOptions } {
  const upstream = new URL(target)
  if (upstream.protocol !== 'http:' || !loopback(upstream.hostname) || upstream.username || upstream.password || upstream.pathname !== '/' || upstream.search || upstream.hash) throw new Error('RPS_API_TARGET must be a loopback HTTP origin')
  function guardOrigin(server: ViteDevServer | PreviewServer) {
    server.middlewares.use((req, res, next) => {
      if (!req.url?.startsWith('/api')) return next()
      if (!validateOrigin(req.headers.host, req.headers.origin, req.method || 'GET', req.headers.cookie, req.headers['sec-fetch-site'] as string | undefined, req.headers.authorization)) {
        res.statusCode = 403; res.setHeader('Content-Type', 'application/json')
        res.end(JSON.stringify({ error: { code: 'origin_forbidden', message: '开发代理拒绝非同源请求' } })); return
      }
      next()
    })
  }
  return {
    guard: {
      name: 'rps-local-api-origin-guard',
      configureServer: guardOrigin,
      configurePreviewServer: guardOrigin,
    },
    proxy: {
      target: upstream.origin, changeOrigin: true,
      configure(proxy) {
        proxy.on('proxyRes', (response, _request, downstream) => {
          if (String(response.headers['content-type']).includes('text/event-stream')) bridgeEventStream(response, downstream)
        })
        proxy.on('proxyReq', (outgoing, incoming) => {
          // The middleware has already validated the original browser origin.
          // Never synthesize Origin for a request which did not supply one.
          if (incoming.headers.origin) outgoing.setHeader('Origin', upstream.origin)
        })
      },
    },
  }
}
