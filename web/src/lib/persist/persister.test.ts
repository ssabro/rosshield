// PWA persist Stage 2 — persister round-trip 단위 테스트
// (design doc `pwa-persist-design.md` §6.2 + §7 Stage 2).
//
// 검증 범위:
//  - `createPersister()` 가 `@tanstack/query-async-storage-persister` 호환
//    Persister 객체를 반환(`persistClient`/`restoreClient`/`removeClient`).
//  - tenant key 분리 — tenant 별 persister가 별 key namespace 사용.
//  - `persistClient` 후 `restoreClient` 라운드트립 — 동일 캐시 복원.
//  - `removeClient` 후 `restoreClient` 는 `undefined` 반환(persister 표준).
//  - `PERSIST_OPTIONS_BASE` — maxAge 7일 + dehydrate filter 결선.

import { describe, expect, it, beforeEach } from 'vitest'
import 'fake-indexeddb/auto'

import type { PersistedClient } from '@tanstack/react-query-persist-client'

import { createIdbAsyncStorage, persistKeyForTenant } from './idb-storage'
import {
  PERSIST_OPTIONS_BASE,
  PERSIST_MAX_AGE_MS,
  createPersister,
} from './persister'
import { shouldDehydrateQuery } from './dehydrate-filter'

beforeEach(async () => {
  // 매 테스트 시작 전 우리 namespace key 비움 — 테스트 간 격리.
  const storage = createIdbAsyncStorage()
  await Promise.all([
    storage.removeItem(persistKeyForTenant()),
    storage.removeItem(persistKeyForTenant('t1')),
    storage.removeItem(persistKeyForTenant('t2')),
  ])
})

describe('createPersister — Persister 인터페이스 호환', () => {
  it('persistClient / restoreClient / removeClient 함수 노출', () => {
    const persister = createPersister()
    expect(typeof persister.persistClient).toBe('function')
    expect(typeof persister.restoreClient).toBe('function')
    expect(typeof persister.removeClient).toBe('function')
  })
})

describe('createPersister — round-trip', () => {
  it('persist → restore 라운드트립 — 동일 client 복원', async () => {
    const persister = createPersister()
    // PersistedClient 최소 형태 — react-query 내부 형식 모사.
    // status 필드는 union type이므로 `as const` + PersistedClient cast로 정합.
    const client: PersistedClient = {
      timestamp: Date.now(),
      buster: 'test',
      clientState: {
        mutations: [],
        queries: [
          {
            queryKey: ['robots'],
            queryHash: '["robots"]',
            state: {
              data: [{ id: 'r-1', name: 'robot-1' }],
              dataUpdateCount: 1,
              dataUpdatedAt: Date.now(),
              error: null,
              errorUpdateCount: 0,
              errorUpdatedAt: 0,
              fetchFailureCount: 0,
              fetchFailureReason: null,
              fetchMeta: null,
              isInvalidated: false,
              status: 'success',
              fetchStatus: 'idle',
            },
          },
        ],
      },
    }

    await persister.persistClient(client)
    const restored = await persister.restoreClient()

    expect(restored).toBeDefined()
    expect(restored?.buster).toBe('test')
    expect(restored?.clientState.queries).toHaveLength(1)
    expect(restored?.clientState.queries[0]?.queryKey).toEqual(['robots'])
  })

  it('removeClient 후 restoreClient는 undefined', async () => {
    const persister = createPersister()
    await persister.persistClient({
      timestamp: Date.now(),
      buster: 'test',
      clientState: { mutations: [], queries: [] },
    })

    await persister.removeClient()

    const restored = await persister.restoreClient()
    expect(restored).toBeUndefined()
  })

  it('미존재 key restoreClient는 undefined (첫 진입 안전)', async () => {
    // beforeEach가 key를 비웠으므로 첫 restoreClient는 빈 상태.
    const persister = createPersister()
    const restored = await persister.restoreClient()
    expect(restored).toBeUndefined()
  })
})

describe('createPersister — tenant 별 namespace 분리 (D-PWAPER-2)', () => {
  it('tenant 별 persister가 별 데이터 보존 — 누설 0', async () => {
    const persisterT1 = createPersister({ tenantId: 't1' })
    const persisterT2 = createPersister({ tenantId: 't2' })

    await persisterT1.persistClient({
      timestamp: Date.now(),
      buster: 'test',
      clientState: { mutations: [], queries: [] },
    })

    // t1에 영속 후 t2 restore는 없음(빈 상태).
    const restoredT2 = await persisterT2.restoreClient()
    expect(restoredT2).toBeUndefined()

    // t1은 그대로 복원.
    const restoredT1 = await persisterT1.restoreClient()
    expect(restoredT1).toBeDefined()
    expect(restoredT1?.buster).toBe('test')
  })

  it('tenant 미지정 시 anon namespace 사용', async () => {
    const anon = createPersister()
    await anon.persistClient({
      timestamp: Date.now(),
      buster: 'anon-buster',
      clientState: { mutations: [], queries: [] },
    })

    // 동일 anon으로 다시 만든 persister는 동일 key → 복원 OK.
    const anon2 = createPersister()
    const restored = await anon2.restoreClient()
    expect(restored).toBeDefined()
    expect(restored?.buster).toBe('anon-buster')
  })
})

describe('PERSIST_OPTIONS_BASE — maxAge 7일 + dehydrate filter', () => {
  it('maxAge 는 7일 (D-PWAPER-3 권장 default)', () => {
    const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000
    expect(PERSIST_MAX_AGE_MS).toBe(SEVEN_DAYS_MS)
    expect(PERSIST_OPTIONS_BASE.maxAge).toBe(SEVEN_DAYS_MS)
  })

  it('dehydrateOptions.shouldDehydrateQuery 가 보안 차단 list 결선', () => {
    expect(PERSIST_OPTIONS_BASE.dehydrateOptions?.shouldDehydrateQuery).toBe(
      shouldDehydrateQuery,
    )
  })
})
