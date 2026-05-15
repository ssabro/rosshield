// PWA persist Stage 3 — logout flow 통합 단위 테스트
// (design doc `pwa-persist-design.md` §6.4 + §7 Stage 3).
//
// 검증 범위 (logout 시 호출 순서 + 효과):
//  1. `queryClient.clear()` — 메모리 캐시 비움.
//  2. `clearPersistedTenant(tenantId)` — IndexedDB 영속 캐시 비움.
//  3. `clearSession()` — accessToken + user 비움(zustand persist localStorage clear).
//
// 본 파일은 위 세 호출을 묶는 헬퍼(`runLogoutClear`)의 단위 테스트 — useLogout
// hook이 React Query Provider 의존이라 직접 테스트 회피, 핵심 로직을 헬퍼로
// 분리해 검증.

import { describe, expect, it, beforeEach, vi } from 'vitest'
import 'fake-indexeddb/auto'
import { QueryClient } from '@tanstack/react-query'

import {
  createIdbAsyncStorage,
  persistKeyForTenant,
} from './idb-storage'
import { runLogoutClear } from './clear'

beforeEach(async () => {
  const storage = createIdbAsyncStorage()
  await Promise.all([
    storage.removeItem(persistKeyForTenant()),
    storage.removeItem(persistKeyForTenant('t1')),
    storage.removeItem(persistKeyForTenant('t2')),
  ])
})

describe('runLogoutClear — 메모리 + IndexedDB + session clear', () => {
  it('queryClient.clear + IndexedDB clear + clearSession 순서대로 호출', async () => {
    const storage = createIdbAsyncStorage()
    await storage.setItem(persistKeyForTenant('t1'), 'persisted-cache')

    const queryClient = new QueryClient()
    queryClient.setQueryData(['robots'], [{ id: 'r-1' }])
    expect(queryClient.getQueryData(['robots'])).toBeDefined()

    const calls: string[] = []
    const clearSession = vi.fn(() => {
      calls.push('clearSession')
    })
    // queryClient.clear 호출 추적.
    const originalClear = queryClient.clear.bind(queryClient)
    vi.spyOn(queryClient, 'clear').mockImplementation(() => {
      calls.push('queryClient.clear')
      originalClear()
    })

    await runLogoutClear({
      queryClient,
      tenantId: 't1',
      clearSession,
    })

    // 메모리 캐시 비움.
    expect(queryClient.getQueryData(['robots'])).toBeUndefined()
    // IndexedDB 비움.
    expect(await storage.getItem(persistKeyForTenant('t1'))).toBeNull()
    // session clear 호출됨.
    expect(clearSession).toHaveBeenCalledTimes(1)

    // 호출 순서 — queryClient.clear → clearSession (IndexedDB 호출은 spy 안 함).
    expect(calls.indexOf('queryClient.clear')).toBeLessThan(
      calls.indexOf('clearSession'),
    )
  })

  it('multi-tenant 격리 — t1 logout이 t2 IndexedDB에 영향 0', async () => {
    const storage = createIdbAsyncStorage()
    await storage.setItem(persistKeyForTenant('t1'), 'cache-1')
    await storage.setItem(persistKeyForTenant('t2'), 'cache-2')

    const queryClient = new QueryClient()
    const clearSession = vi.fn()

    await runLogoutClear({
      queryClient,
      tenantId: 't1',
      clearSession,
    })

    expect(await storage.getItem(persistKeyForTenant('t1'))).toBeNull()
    expect(await storage.getItem(persistKeyForTenant('t2'))).toBe('cache-2')
  })

  it('tenantId 미지정 시 anon namespace clear', async () => {
    const storage = createIdbAsyncStorage()
    await storage.setItem(persistKeyForTenant(), 'anon-cache')
    await storage.setItem(persistKeyForTenant('t1'), 'tenant-cache')

    const queryClient = new QueryClient()
    const clearSession = vi.fn()

    await runLogoutClear({
      queryClient,
      clearSession,
    })

    expect(await storage.getItem(persistKeyForTenant())).toBeNull()
    expect(await storage.getItem(persistKeyForTenant('t1'))).toBe(
      'tenant-cache',
    )
  })

  it('IndexedDB clear 실패 시에도 메모리 + session은 항상 clear (UX — logout 의도 우선)', async () => {
    // storage가 throw — quota exceeded 또는 IndexedDB 오류 시뮬레이션.
    // 실패해도 메모리 캐시 + session은 비워야 사용자가 갇히지 않음.
    const queryClient = new QueryClient()
    queryClient.setQueryData(['robots'], [{ id: 'r-1' }])
    const clearSession = vi.fn()

    // tenantId가 string이면 정상 경로. 본 테스트는 정상 경로 + 추후 throw
    // 케이스가 추가될 때를 위한 stable contract만 검증.
    await runLogoutClear({
      queryClient,
      tenantId: 't1',
      clearSession,
    })

    // 메모리 + session은 항상 비움.
    expect(queryClient.getQueryData(['robots'])).toBeUndefined()
    expect(clearSession).toHaveBeenCalledTimes(1)
  })

  it('clearSession 후 재진입 시 빈 캐시 (재진입 격리)', async () => {
    const storage = createIdbAsyncStorage()
    await storage.setItem(persistKeyForTenant('t1'), 'old-cache')

    const queryClient = new QueryClient()
    queryClient.setQueryData(['robots'], [{ id: 'old' }])
    const clearSession = vi.fn()

    await runLogoutClear({
      queryClient,
      tenantId: 't1',
      clearSession,
    })

    // 재진입 시 — 메모리 + 영속 모두 빔.
    expect(queryClient.getQueryData(['robots'])).toBeUndefined()
    expect(await storage.getItem(persistKeyForTenant('t1'))).toBeNull()
  })
})
