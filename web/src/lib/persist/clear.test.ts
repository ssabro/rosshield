// PWA persist Stage 3 — logout 시 IndexedDB clear 헬퍼 단위 테스트
// (design doc `pwa-persist-design.md` §6.4 + §7 Stage 3 + D-PWAPER-4).
//
// 검증 범위:
//  - `clearPersistedTenant(tenantId)` 가 해당 tenant key의 IndexedDB 데이터를
//    제거 (idb-storage.removeItem 위임).
//  - tenant 미지정 시 `anon` namespace clear.
//  - **multi-tenant 격리** — tenant A clear가 tenant B persist 데이터에 영향 0
//    (D-PWAPER-2 + D-PWAPER-4 정합).
//  - clear 후 동일 tenant restore는 빈 캐시.
//  - persister.removeClient와 동등 결과 — Stage 2 persister 라운드트립과 호환.

import { describe, expect, it, beforeEach } from 'vitest'
import 'fake-indexeddb/auto'

import {
  createIdbAsyncStorage,
  persistKeyForTenant,
} from './idb-storage'
import { createPersister } from './persister'
import { clearPersistedTenant } from './clear'

beforeEach(async () => {
  // 매 테스트 시작 전 우리 namespace key 비움 — 테스트 간 격리.
  const storage = createIdbAsyncStorage()
  await Promise.all([
    storage.removeItem(persistKeyForTenant()),
    storage.removeItem(persistKeyForTenant('t1')),
    storage.removeItem(persistKeyForTenant('t2')),
  ])
})

describe('clearPersistedTenant — 기본 동작', () => {
  it('지정한 tenantId의 IndexedDB key 제거', async () => {
    const storage = createIdbAsyncStorage()
    await storage.setItem(persistKeyForTenant('t1'), 'cached-data-t1')

    // clear 호출 — Promise resolve 정상.
    await clearPersistedTenant('t1')

    // 해당 tenant key는 비어있음.
    const after = await storage.getItem(persistKeyForTenant('t1'))
    expect(after).toBeNull()
  })

  it('tenantId 미지정 시 anon namespace clear', async () => {
    const storage = createIdbAsyncStorage()
    await storage.setItem(persistKeyForTenant(), 'anon-data')

    await clearPersistedTenant()

    expect(await storage.getItem(persistKeyForTenant())).toBeNull()
  })

  it('null/undefined/빈 문자열 모두 anon으로 처리', async () => {
    const storage = createIdbAsyncStorage()
    await storage.setItem(persistKeyForTenant(), 'anon-data')

    await clearPersistedTenant(null)
    expect(await storage.getItem(persistKeyForTenant())).toBeNull()

    await storage.setItem(persistKeyForTenant(), 'anon-data-2')
    await clearPersistedTenant(undefined)
    expect(await storage.getItem(persistKeyForTenant())).toBeNull()

    await storage.setItem(persistKeyForTenant(), 'anon-data-3')
    await clearPersistedTenant('')
    expect(await storage.getItem(persistKeyForTenant())).toBeNull()
  })

  it('미존재 tenant key clear는 멱등 (throw 0)', async () => {
    // setItem 없이 바로 clear — idb-keyval del은 미존재 키도 멱등 처리.
    await expect(clearPersistedTenant('never-set')).resolves.toBeUndefined()
  })
})

describe('clearPersistedTenant — multi-tenant 격리 (D-PWAPER-4)', () => {
  it('tenant A clear가 tenant B 데이터에 영향 0', async () => {
    const storage = createIdbAsyncStorage()
    await storage.setItem(persistKeyForTenant('t1'), 'tenant-1-cache')
    await storage.setItem(persistKeyForTenant('t2'), 'tenant-2-cache')

    // tenant A logout 시뮬레이션.
    await clearPersistedTenant('t1')

    // t1만 비고 t2는 그대로.
    expect(await storage.getItem(persistKeyForTenant('t1'))).toBeNull()
    expect(await storage.getItem(persistKeyForTenant('t2'))).toBe(
      'tenant-2-cache',
    )
  })

  it('anon clear가 tenant 데이터에 영향 0 (로그인 전 → 로그인 시나리오 보호)', async () => {
    const storage = createIdbAsyncStorage()
    await storage.setItem(persistKeyForTenant(), 'anon-cache')
    await storage.setItem(persistKeyForTenant('t1'), 'tenant-1-cache')

    await clearPersistedTenant()

    expect(await storage.getItem(persistKeyForTenant())).toBeNull()
    expect(await storage.getItem(persistKeyForTenant('t1'))).toBe(
      'tenant-1-cache',
    )
  })
})

describe('clearPersistedTenant — Stage 2 persister와 호환', () => {
  it('persister.persistClient 후 clearPersistedTenant → restoreClient는 undefined', async () => {
    const persister = createPersister({ tenantId: 't1' })

    // 정상 영속.
    await persister.persistClient({
      timestamp: Date.now(),
      buster: 'test',
      clientState: { mutations: [], queries: [] },
    })

    // 정상 복원 확인 (sanity).
    expect(await persister.restoreClient()).toBeDefined()

    // logout 시 헬퍼 호출.
    await clearPersistedTenant('t1')

    // 빈 캐시 확인 — 재 진입 시 fresh state.
    expect(await persister.restoreClient()).toBeUndefined()
  })

  it('logout 후 동일 사용자 재진입 시 빈 캐시 (재진입 격리)', async () => {
    // 1차 세션.
    const p1 = createPersister({ tenantId: 't1' })
    await p1.persistClient({
      timestamp: Date.now(),
      buster: 'session-1',
      clientState: { mutations: [], queries: [] },
    })

    // logout.
    await clearPersistedTenant('t1')

    // 2차 세션 — 같은 tenant + 같은 origin이지만 캐시 새로.
    const p2 = createPersister({ tenantId: 't1' })
    expect(await p2.restoreClient()).toBeUndefined()
  })
})
