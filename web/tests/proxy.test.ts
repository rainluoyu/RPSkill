import { EventEmitter } from 'node:events'
import type { IncomingMessage, ServerResponse } from 'node:http'
import { describe, expect, it, vi } from 'vitest'
import { bridgeEventStream, devProxy, validateOrigin } from '../config/dev-proxy'
describe('local Vite API security boundary', () => {
  it('accepts same-origin browser mutations and cookie GET/SSE requests', () => {
    expect(validateOrigin('127.0.0.1:5173', 'http://127.0.0.1:5173', 'POST', 'rps_session_8717=secret', 'same-origin', undefined)).toBe(true)
    expect(validateOrigin('127.0.0.1:5173', undefined, 'GET', 'rps_session_8717=secret', 'same-origin', undefined)).toBe(true)
  })
  it.each(['http://evil.example', 'null', 'http://127.0.0.1:8717', 'https://127.0.0.1:5173'])('rejects original cross-origin requests before proxy rewriting: %s', (origin) => {
    expect(validateOrigin('127.0.0.1:5173', origin, 'POST', undefined, undefined, 'Bearer token')).toBe(false)
  })
  it('does not synthesize missing Origin for cookie mutations', () => {
    expect(validateOrigin('127.0.0.1:5173', undefined, 'POST', 'rps_session_8717=secret', 'same-origin', undefined)).toBe(false)
  })
  it('blocks DNS rebinding and cross-site fetch metadata', () => {
    expect(validateOrigin('evil.example:5173', undefined, 'GET', undefined, undefined, undefined)).toBe(false)
    expect(validateOrigin('127.0.0.1:5173', undefined, 'GET', undefined, 'cross-site', undefined)).toBe(false)
  })
  it('applies the same original-origin guard to preview and development', () => {
    const { guard } = devProxy('http://127.0.0.1:8717')
    expect(guard.configureServer).toBe(guard.configurePreviewServer)
  })
  it.each(['https://127.0.0.1:8717', 'http://example.com', 'http://user:pass@localhost', 'http://localhost/api'])('rejects unsafe upstreams: %s', (url) => expect(() => devProxy(url)).toThrow())
})

describe('SSE proxy lifecycle', () => {
  function streams() {
    const source = Object.assign(new EventEmitter(), { complete: false, destroyed: false, destroy: vi.fn() })
    const destination = Object.assign(new EventEmitter(), { destroyed: false, writableEnded: false, destroy: vi.fn() })
    bridgeEventStream(source as unknown as IncomingMessage, destination as unknown as ServerResponse)
    return { source, destination }
  }
  it.each(['aborted', 'error', 'close'])('closes downstream after upstream %s instead of leaving a false-open EventSource', (event) => {
    const { source, destination } = streams()
    source.emit(event)
    expect(destination.destroy).toHaveBeenCalledOnce()
  })
  it('does not abruptly destroy an already completed response', () => {
    const { source, destination } = streams(); source.complete = true; source.emit('close')
    expect(destination.destroy).not.toHaveBeenCalled()
  })
  it('releases upstream subscription when browser closes downstream', () => {
    const { source, destination } = streams(); destination.emit('close')
    expect(source.destroy).toHaveBeenCalledOnce()
  })
})
