// C6 — 401 자동 refresh middleware의 핵심 helper 단위 테스트.
//
// middleware 자체는 openapi-fetch 내부에 등록돼 있어 직접 테스트가 어려움 —
// 그 대신 (a) callRefresh가 store를 갱신·실패 시 null을 반환하는지, (b) makeDedupe가
// 동시 호출에서 in-flight Promise를 공유하는지를 검증한다.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { callRefresh, makeDedupe } from './client'
import { useAuthStore } from '@/stores/auth'

beforeEach(() => {
  useAuthStore.getState().clearSession()
  vi.unstubAllGlobals()
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

// callRefresh가 200 + accessToken 응답을 받으면 store를 갱신하고 새 token을 반환.
describe('callRefresh', () => {
  it('200 응답 + accessToken → store 갱신 + token 반환', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ accessToken: 'new-token-123' }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    const out = await callRefresh()
    expect(out).toBe('new-token-123')
    expect(useAuthStore.getState().accessToken).toBe('new-token-123')

    // 호출 인자 검증 — endpoint·credentials·X-Cookie-Auth 헤더.
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toMatch(/\/api\/v1\/auth\/refresh$/)
    expect(init.method).toBe('POST')
    expect(init.credentials).toBe('include')
    expect(init.headers['X-Cookie-Auth']).toBe('true')
  })

  it('401 응답 → null 반환, store 미변경', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
      json: async () => ({}),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    const out = await callRefresh()
    expect(out).toBeNull()
    expect(useAuthStore.getState().accessToken).toBeNull()
  })

  it('200이지만 accessToken 누락 → null', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({}),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    const out = await callRefresh()
    expect(out).toBeNull()
    expect(useAuthStore.getState().accessToken).toBeNull()
  })

  it('네트워크 throw → null (catch 처리)', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('network error'))
    vi.stubGlobal('fetch', fetchMock)

    const out = await callRefresh()
    expect(out).toBeNull()
  })
})

// makeDedupe — 동시 호출 dedupe + 끝난 후 reset.
describe('makeDedupe', () => {
  it('동시 호출은 한 번만 실행되고 같은 결과를 공유', async () => {
    let resolveCall: (v: string) => void = () => {}
    const inner = vi.fn().mockImplementation(
      () =>
        new Promise<string>((resolve) => {
          resolveCall = resolve
        }),
    )
    const dedupe = makeDedupe(inner)

    const p1 = dedupe()
    const p2 = dedupe()
    const p3 = dedupe()

    expect(inner).toHaveBeenCalledTimes(1)

    resolveCall('shared-result')
    const [a, b, c] = await Promise.all([p1, p2, p3])
    expect(a).toBe('shared-result')
    expect(b).toBe('shared-result')
    expect(c).toBe('shared-result')
  })

  it('첫 호출이 끝난 뒤 다음 호출은 새로 실행', async () => {
    const inner = vi
      .fn()
      .mockResolvedValueOnce('first')
      .mockResolvedValueOnce('second')
    const dedupe = makeDedupe(inner)

    const out1 = await dedupe()
    const out2 = await dedupe()
    expect(inner).toHaveBeenCalledTimes(2)
    expect(out1).toBe('first')
    expect(out2).toBe('second')
  })

  it('실패도 in-flight 내에서 공유되며 reset됨', async () => {
    const inner = vi
      .fn()
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce('ok')
    const dedupe = makeDedupe(inner)

    await expect(dedupe()).rejects.toThrow('boom')
    // reset 됐으므로 다음 호출은 inner를 새로 호출.
    await expect(dedupe()).resolves.toBe('ok')
    expect(inner).toHaveBeenCalledTimes(2)
  })
})
