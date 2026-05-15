// PWA persist Stage 3 — logout 시 IndexedDB clear 헬퍼
// (design doc `pwa-persist-design.md` §6.4 + §7 Stage 3 + D-PWAPER-4).
//
// 책임:
//  - `clearPersistedTenant(tenantId)` — 지정 tenant의 IndexedDB 영속 캐시
//    제거(persister.removeClient와 동등 효과). queryClient/persister 미참조 →
//    레이어 분리(`web/src/api/client.ts`처럼 React Query Provider 부재 영역에서도
//    호출 가능).
//  - `runLogoutClear(opts)` — useLogout(`web/src/api/hooks.ts`)에서 호출하는
//    상위 헬퍼. 다음 순서로 실행:
//      1. `queryClient.clear()` — 메모리 캐시 비움(즉시 UI 반영).
//      2. `clearPersistedTenant(tenantId)` — IndexedDB 영속 캐시 비움.
//      3. `clearSession()` — accessToken + user 비움(zustand persist localStorage clear).
//
// 호출 시점 (D-PWAPER-4 권장 default):
//  - `useLogout` mutation onSuccess 또는 mutationFn 내부 — logout API 응답 후.
//  - `client.ts` 401 refresh 실패 직후 — `clearPersistedTenant`만 호출(queryClient
//    미접근). UX는 `clearSession`이 router 재진입을 트리거.
//
// multi-tenant 격리 (D-PWAPER-2 + D-PWAPER-4):
//  - tenant 별 IndexedDB key는 Stage 1 `persistKeyForTenant`로 자동 분리.
//  - 본 헬퍼는 명시 tenant key만 clear → 다른 tenant 캐시에 영향 0.
//  - tenant 전환(SSO 다중 tenant)은 D-PWAPER-2 새 persister 인스턴스 자동 분리
//    + 명시 clear 불필요(orphan key는 maxAge 7일에 자연 만료 — D-PWAPER-3).

import type { QueryClient } from '@tanstack/react-query'

import {
  createIdbAsyncStorage,
  persistKeyForTenant,
} from './idb-storage'

/**
 * 지정 tenant의 IndexedDB 영속 캐시를 제거합니다.
 *
 * persister.removeClient와 동등한 효과 — 단, 호출처에서 persister 인스턴스를
 * 가지지 않아도 호출 가능(레이어 분리). 미존재 키 삭제는 멱등.
 *
 * @param tenantId tenant ID. 미지정/null/빈 문자열 → `anon` namespace clear.
 *
 * 동작:
 *  - `persistKeyForTenant(tenantId)`로 storage key 결정.
 *  - default IndexedDB store(`rosshield-rq` DB)의 해당 key 제거.
 *  - 내부 storage가 throw하면 그대로 전파 — 호출처(useLogout 등)에서 try/catch.
 *    UX 폴백은 호출처 책임(예: useLogout은 IndexedDB 실패에도 메모리 + session
 *    은 항상 clear).
 */
export async function clearPersistedTenant(
  tenantId?: string | null,
): Promise<void> {
  const storage = createIdbAsyncStorage()
  const key = persistKeyForTenant(tenantId)
  await storage.removeItem(key)
}

/** `runLogoutClear` 호출 옵션. */
export interface RunLogoutClearOptions {
  /**
   * react-query QueryClient 인스턴스. `clear()` 호출로 메모리 캐시 전체 제거.
   * 호출처(`useLogout`)가 `useQueryClient()`로 획득해 전달.
   */
  queryClient: QueryClient
  /**
   * 영속 캐시 clear 대상 tenant ID. logout 시점의 user.tenantId.
   * 미지정 시 `anon` namespace clear(로그인 전 상태에서 호출 안전).
   */
  tenantId?: string | null
  /**
   * zustand auth store의 `clearSession` 함수.
   * 호출처가 `useAuthStore.getState().clearSession`로 전달.
   *
   * **호출 순서**: queryClient.clear → IndexedDB clear → clearSession.
   * clearSession이 가장 마지막인 이유 — App.tsx의 `useMemo` persister
   * 의존성(tenantId)이 갱신되어 새 persister(anon namespace)로 자동 전환됨.
   * persister 전환 전에 IndexedDB clear가 끝나야 race 0.
   */
  clearSession: () => void
}

/**
 * logout flow의 메모리/영속/세션 3단계 clear를 묶어 실행합니다.
 *
 * 호출 순서 (보안 + UX 정합):
 *  1. `queryClient.clear()` — 메모리 캐시 즉시 비움. UI는 다음 렌더에 빈 상태로
 *     전환 → 로그아웃 직후 stale 데이터 노출 0.
 *  2. `clearPersistedTenant(tenantId)` — IndexedDB 영속 캐시 비움. 다음 진입 시
 *     hydrate 빈 상태.
 *  3. `clearSession()` — accessToken + user 비움(zustand persist). router는
 *     accessToken 부재 → 로그인 페이지로 자동 redirect.
 *
 * IndexedDB clear가 throw해도 메모리 + session은 항상 clear되도록 try/catch
 * 처리(UX — 사용자 의도는 로그아웃, 갇히지 않게).
 */
export async function runLogoutClear(
  opts: RunLogoutClearOptions,
): Promise<void> {
  const { queryClient, tenantId, clearSession } = opts

  // 1. 메모리 캐시 비움 — 즉시 효과(다음 렌더에 빈 데이터).
  queryClient.clear()

  // 2. IndexedDB 영속 캐시 비움 — 다음 진입 시 hydrate 빈 상태.
  //    실패해도 메모리 + session은 항상 clear되도록 swallow.
  try {
    await clearPersistedTenant(tenantId)
  } catch (err) {
    // 운영자 진단용 — IndexedDB quota 또는 corruption 시.
    console.warn('[rosshield] persist clear 실패 (logout 진행):', err)
  }

  // 3. session clear — router 재진입 트리거.
  clearSession()
}
