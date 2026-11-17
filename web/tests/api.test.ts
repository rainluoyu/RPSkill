import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, consumeFragment } from '../src/lib/api'
import { response } from './fixtures'
beforeEach(() => { vi.stubGlobal('fetch', vi.fn()) })
afterEach(() => vi.unstubAllGlobals())
describe('API authentication and mutation contract', () => {
  it('consumes and immediately clears a fragment without writing browser storage', () => {
    history.replaceState(null, '', '/?view=work#token=secret-value')
    expect(consumeFragment()).toBe('secret-value'); expect(location.hash).toBe(''); expect(location.search).toBe('?view=work')
    expect(localStorage.length).toBe(0); expect(sessionStorage.length).toBe(0)
  })
  it('preserves ordinary navigation fragments without treating them as credentials', () => {
    history.replaceState(null, '', '/#main-content')
    expect(consumeFragment()).toBe(''); expect(location.hash).toBe('#main-content')
  })
  it('sends same-origin cookies and strict JSON envelopes', async () => {
    vi.mocked(fetch).mockResolvedValue(response({ deleted: true }))
    expect(await api('/characters/alice', 'DELETE', {})).toEqual({ deleted: true })
    const [, options] = vi.mocked(fetch).mock.calls[0]!
    expect(options?.credentials).toBe('same-origin'); expect(options?.body).toBe('{}')
    expect(options?.headers).toMatchObject({ 'Content-Type': 'application/json' })
  })
  it('never automatically repeats a mutation after an ambiguous network error', async () => {
    vi.mocked(fetch).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(response({ ok: true }))
    await expect(api('/project', 'PATCH', { name: 'new' })).rejects.toMatchObject({ code: 'network_error' })
    expect(fetch).toHaveBeenCalledTimes(1)
    await api('/project', 'PATCH', { name: 'new' })
    const calls = vi.mocked(fetch).mock.calls
    expect(calls[0]?.[1]?.headers).toEqual(calls[1]?.[1]?.headers)
  })
  it('reports a rejected write without retaining a receipt', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'validation_error', message: 'invalid' } }), { status: 422 })).mockResolvedValueOnce(response({}))
    await expect(api('/project', 'PATCH', {})).rejects.toMatchObject({ code: 'validation_error', status: 422 })
    await api('/project', 'PATCH', {})
    expect(vi.mocked(fetch).mock.calls[0]?.[1]?.headers).toEqual(vi.mocked(fetch).mock.calls[1]?.[1]?.headers)
  })
})
