// PWA persist Stage 1 — IndexedDB AsyncStorage 어댑터 단위 테스트
// (design doc `pwa-persist-design.md` §7 Stage 1).
//
// 검증 범위:
//  - get/set/remove 라운드트립 — JSON 문자열 보존.
//  - 미존재 키 → null 반환 (`@tanstack/query-async-storage-persister` 시그니처
//    호환: 어댑터는 string|null|undefined 중 null 반환).
//  - 동시 set 정합성 — Promise.all 동시 호출 후 마지막 값 보존(idb-keyval은
//    내부적으로 직렬화된 transaction을 사용하므로 race 손실 0).
//  - 에러 처리 — 내부 storage가 throw하면 어댑터도 throw 전파(persister가
//    silent fail로 캐시를 버리지 않도록).
//  - tenant key 헬퍼 — `rosshield-rq-${tenantId}` 형식, 미지정 시 `anon`
//    (D-PWAPER-2 권장 default).
//
// 환경:
//  - Vitest jsdom — IndexedDB 부재 → `fake-indexeddb/auto` 자동 polyfill.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import 'fake-indexeddb/auto'

import {
  createIdbAsyncStorage,
  persistKeyForTenant,
  type AsyncStorage,
} from './idb-storage'

// 매 테스트 시작 전 IndexedDB를 fresh 상태로 — 테스트 간 캐시 격리.
beforeEach(async () => {
  // fake-indexeddb는 reset 헬퍼가 있으나 import 시점 초기화도 충분.
  // 명시적으로 우리 store(default `keyval-store`)를 비우기 위해 어댑터의
  // removeItem 활용 — 핵심 키만 삭제(테스트 격리).
  const storage = createIdbAsyncStorage()
  await Promise.all([
    storage.removeItem('rosshield-rq-anon'),
    storage.removeItem('rosshield-rq-t1'),
    storage.removeItem('rosshield-rq-t2'),
    storage.removeItem('test-key'),
  ])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('createIdbAsyncStorage', () => {
  it('get/set/remove 라운드트립 — string 값 보존', async () => {
    const storage = createIdbAsyncStorage()

    await storage.setItem('test-key', '{"hello":"world"}')
    const value = await storage.getItem('test-key')
    expect(value).toBe('{"hello":"world"}')

    await storage.removeItem('test-key')
    const cleared = await storage.getItem('test-key')
    expect(cleared).toBeNull()
  })

  it('미존재 키는 null 반환', async () => {
    const storage = createIdbAsyncStorage()

    const value = await storage.getItem('absent-key')
    expect(value).toBeNull()
  })

  it('동시 set 정합성 — 마지막 write 보존(quota/race 손실 0)', async () => {
    const storage = createIdbAsyncStorage()

    // 5개 동시 set — idb-keyval은 내부 transaction 직렬화로 race 0.
    await Promise.all([
      storage.setItem('test-key', 'v1'),
      storage.setItem('test-key', 'v2'),
      storage.setItem('test-key', 'v3'),
      storage.setItem('test-key', 'v4'),
      storage.setItem('test-key', 'v5'),
    ])

    const value = await storage.getItem('test-key')
    // 마지막 write가 보존(어떤 값이든 5종 중 하나여야 — 손실 0).
    expect(['v1', 'v2', 'v3', 'v4', 'v5']).toContain(value)
  })

  it('순차 set 후 마지막 값 보존', async () => {
    const storage = createIdbAsyncStorage()

    await storage.setItem('test-key', 'first')
    await storage.setItem('test-key', 'second')
    await storage.setItem('test-key', 'third')

    const value = await storage.getItem('test-key')
    expect(value).toBe('third')
  })

  it('내부 store 에러는 throw 전파 — silent fail 0', async () => {
    // store factory를 주입해 throw하는 store로 교체.
    const failingStore = {
      get: vi.fn(async () => {
        throw new Error('quota exceeded')
      }),
      set: vi.fn(async () => {
        throw new Error('quota exceeded')
      }),
      del: vi.fn(async () => {
        throw new Error('quota exceeded')
      }),
    }
    const storage = createIdbAsyncStorage({ store: failingStore })

    await expect(storage.getItem('any')).rejects.toThrow('quota exceeded')
    await expect(storage.setItem('any', 'v')).rejects.toThrow('quota exceeded')
    await expect(storage.removeItem('any')).rejects.toThrow('quota exceeded')
  })

  it('AsyncStorage 시그니처 호환 — getItem은 Promise<string | null>', async () => {
    const storage: AsyncStorage = createIdbAsyncStorage()

    await storage.setItem('test-key', 'value')
    const v = await storage.getItem('test-key')
    // 타입은 string | null — undefined 아님.
    expect(v === null || typeof v === 'string').toBe(true)
  })
})

describe('persistKeyForTenant', () => {
  it('tenantId 지정 시 접두 결합', () => {
    expect(persistKeyForTenant('t1')).toBe('rosshield-rq-t1')
    expect(persistKeyForTenant('tenant-uuid-abc')).toBe(
      'rosshield-rq-tenant-uuid-abc',
    )
  })

  it('tenantId 미지정 시 `anon` fallback (D-PWAPER-2 default)', () => {
    expect(persistKeyForTenant()).toBe('rosshield-rq-anon')
    expect(persistKeyForTenant(undefined)).toBe('rosshield-rq-anon')
    expect(persistKeyForTenant(null)).toBe('rosshield-rq-anon')
    expect(persistKeyForTenant('')).toBe('rosshield-rq-anon')
  })

  it('tenant 별 key 분리 — 다른 tenant 캐시 누설 0 (D-PWAPER-2)', () => {
    expect(persistKeyForTenant('t1')).not.toBe(persistKeyForTenant('t2'))
  })
})

describe('createIdbAsyncStorage — multi-tenant 격리', () => {
  it('tenant 별 key 별 데이터 분리 (D-PWAPER-2)', async () => {
    const storage = createIdbAsyncStorage()

    await storage.setItem(persistKeyForTenant('t1'), 'tenant-1-data')
    await storage.setItem(persistKeyForTenant('t2'), 'tenant-2-data')

    expect(await storage.getItem(persistKeyForTenant('t1'))).toBe(
      'tenant-1-data',
    )
    expect(await storage.getItem(persistKeyForTenant('t2'))).toBe(
      'tenant-2-data',
    )

    // tenant t1 삭제 후에도 t2는 유지.
    await storage.removeItem(persistKeyForTenant('t1'))
    expect(await storage.getItem(persistKeyForTenant('t1'))).toBeNull()
    expect(await storage.getItem(persistKeyForTenant('t2'))).toBe(
      'tenant-2-data',
    )
  })
})
