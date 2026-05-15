// PWA persist Stage 2 — react-query AsyncStorage persister 결선
// (design doc `pwa-persist-design.md` §6.2 + §7 Stage 2).
//
// 책임:
//  - `createAsyncStoragePersister` 팩토리 + IndexedDB AsyncStorage 어댑터(Stage 1).
//  - tenant 별 storage key 자동 분리(D-PWAPER-2 — 단일 origin SSO 다중 tenant 누설 차단).
//  - `PersistQueryClientProvider`에 전달할 공용 옵션(`PERSIST_OPTIONS_BASE`):
//    - `maxAge` 7일(D-PWAPER-3) — 7일 초과 캐시는 hydrate 시 자동 무시.
//    - `dehydrateOptions.shouldDehydrateQuery` — 보안 차단 list(D-PWAPER-5).
//  - serialize/deserialize는 persister default(JSON.stringify/parse) 유지 —
//    명시 옵션 미설정 = 표준 동작 + 추가 의존 0.
//
// buster(D-PWAPER-6 — build hash 기반 캐시 무효화):
//  - 본 Stage 2는 buster 미결선. `import.meta.env.VITE_BUILD_HASH` 결선은 후속
//    epic(또는 Stage 3 직전)에서 vite.config.ts define 추가와 동시 진행.
//  - 미결선 상태에서도 maxAge 7일이 사실상 buster 역할 충족(stale 캐시 자동 만료).
//
// 비대상:
//  - logout 시 `removeClient` 호출(Stage 3 — 별 commit).
//  - StaleDataBadge UX(D-PWAPER-7) — 후속 epic.

import { createAsyncStoragePersister } from '@tanstack/query-async-storage-persister'
import type { PersistedClient, Persister } from '@tanstack/react-query-persist-client'

import { createIdbAsyncStorage, persistKeyForTenant } from './idb-storage'
import { shouldDehydrateQuery } from './dehydrate-filter'

/** 캐시 만료(7일) — D-PWAPER-3 권장 default. */
export const PERSIST_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000

/**
 * `PersistQueryClientProvider` 에 전달할 공용 옵션.
 *
 * 사용처(`App.tsx`):
 * ```ts
 * <PersistQueryClientProvider
 *   client={queryClient}
 *   persistOptions={{ persister, ...PERSIST_OPTIONS_BASE }}
 * >
 * ```
 *
 * 주의: `persister`는 tenant 별로 다르므로 본 base에는 미포함. 호출처가
 * `createPersister({ tenantId })` 결과를 spread 합성.
 */
export const PERSIST_OPTIONS_BASE = {
  maxAge: PERSIST_MAX_AGE_MS,
  dehydrateOptions: {
    shouldDehydrateQuery,
  },
} as const

export interface CreatePersisterOptions {
  /**
   * tenant ID — 미지정 시 `anon` namespace(로그인 전 또는 tenantId 모름).
   * tenant 변경 시 `useMemo` 등으로 새 persister 인스턴스 생성 권장.
   */
  tenantId?: string | null
}

/**
 * IndexedDB AsyncStorage 어댑터 기반 react-query persister 생성.
 *
 * @returns `@tanstack/react-query-persist-client` 의 `Persister` 호환 객체
 *   (`persistClient` / `restoreClient` / `removeClient`).
 *
 * 동작:
 *  - tenant 별 storage key — `persistKeyForTenant(tenantId)`로 분리.
 *  - serialize/deserialize는 persister default(JSON.stringify/parse) — 명시
 *    옵션 미설정으로 표준 동작 + 의존 최소.
 *  - throttleTime은 persister default(1000ms) 유지 — 빈번 mutation 시 IndexedDB
 *    쓰기 부하 자동 흡수.
 */
export function createPersister(
  opts: CreatePersisterOptions = {},
): Persister {
  const storage = createIdbAsyncStorage()
  const key = persistKeyForTenant(opts.tenantId)
  return createAsyncStoragePersister({
    storage,
    key,
  })
}

// 호환 re-export — App.tsx가 단일 모듈에서 import 가능하도록.
export type { PersistedClient, Persister }
