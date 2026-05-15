// PWA persist Stage 1 — IndexedDB AsyncStorage 어댑터
// (design doc `pwa-persist-design.md` §6 outline + §7 Stage 1).
//
// 목적:
//  - `@tanstack/query-async-storage-persister`가 요구하는 AsyncStorage 시그니처
//    (`getItem` / `setItem` / `removeItem`)를 IndexedDB로 충족합니다.
//  - 채택 backing store = **IndexedDB** (D-PWAPER-1 권장 default — 50MB+ quota,
//    메인 스레드 비차단). localStorage 5MB 한계 회피 + idb-keyval(~3KB)의 검증된
//    transaction 직렬화 활용.
//
// 의존:
//  - `idb-keyval` — Jake Archibald의 ~3KB IndexedDB key-value wrapper. 자체 store
//    factory(`createStore`)로 origin scope DB명/object store명 명시 가능.
//
// 비대상 (Stage 2~4):
//  - `App.tsx` `PersistQueryClientProvider` 결선 — Stage 2.
//  - 민감 데이터 차단 list(`shouldDehydrateQuery`) — Stage 2~3 (`persist-query.ts`).
//  - logout 시 `removeClient` 결선 — Stage 3.
//  - 운영자 docs `docs/operations/pwa-offline.md` §3 추가 — Stage 4.
//
// 멀티테넌시 (D-PWAPER-2):
//  - `persistKeyForTenant(tenantId)` 헬퍼로 tenant 별 key 접두 분리. SSO 다중
//    tenant + 단일 origin 시나리오에서 사용자 A↔B 캐시 누설 사전 차단.

import {
  createStore,
  del,
  get,
  set,
  type UseStore,
} from 'idb-keyval'

/**
 * `@tanstack/query-async-storage-persister`가 요구하는 비동기 storage 인터페이스.
 *
 * 시그니처는 의도적으로 좁힘:
 *  - `getItem`은 `string | null` 반환(미존재 키 = null).
 *  - `setItem`은 string 값만 — persister는 직렬화된 JSON 문자열을 전달.
 *  - `removeItem`은 멱등(미존재 키 삭제도 OK).
 */
export interface AsyncStorage {
  getItem(key: string): Promise<string | null>
  setItem(key: string, value: string): Promise<void>
  removeItem(key: string): Promise<void>
}

/** 어댑터 생성 옵션 — 기본값은 origin scope `rosshield-rq` DB. */
export interface IdbAsyncStorageOptions {
  /**
   * 사용자 정의 store. 미지정 시 기본 store(`rosshield-rq` DB + `keyval`
   * object store)를 origin scope에 단일 생성.
   * 테스트 또는 진단 시 throw하는 mock store 주입 가능.
   */
  store?: IdbStore
}

/**
 * idb-keyval `UseStore`와 동등한 최소 인터페이스(테스트 mock 친화).
 *
 * 내부적으로 `idb-keyval`의 `get(key, store)` / `set(key, value, store)` /
 * `del(key, store)` 호출에 그대로 전달. `UseStore`(callback 형식) 또는 동등한
 * async 함수 객체를 허용하기 위해 union으로 둠.
 */
export type IdbStore =
  | UseStore
  | {
      get(key: string): Promise<unknown>
      set(key: string, value: unknown): Promise<void>
      del(key: string): Promise<void>
    }

// 모듈 단위 default store — origin scope 단일 IndexedDB DB/object store.
// 매 호출마다 store를 새로 만들지 않도록 lazy singleton.
let defaultStoreSingleton: UseStore | null = null

function getDefaultStore(): UseStore {
  if (defaultStoreSingleton === null) {
    // DB명 `rosshield-rq` + object store명 `keyval` — 운영자가 DevTools
    // Application > IndexedDB에서 식별 가능한 명명 규칙.
    defaultStoreSingleton = createStore('rosshield-rq', 'keyval')
  }
  return defaultStoreSingleton
}

/**
 * IndexedDB 기반 AsyncStorage 어댑터를 생성합니다.
 *
 * @param opts.store 사용자 정의 store(테스트 mock 또는 별 DB 주입). 미지정 시
 *   origin scope 기본 store(`rosshield-rq` DB).
 *
 * 동작:
 *  - `getItem(k)` → 미존재 키일 경우 `undefined` 대신 **null** 반환(persister 호환).
 *  - `setItem(k, v)` → idb-keyval `set` 위임. quota exceeded 등 내부 에러는
 *    그대로 throw 전파(silent fail 0 — persister가 stale 캐시를 부분 손상 상태로
 *    유지하지 않도록).
 *  - `removeItem(k)` → idb-keyval `del` 위임. 멱등.
 */
export function createIdbAsyncStorage(
  opts: IdbAsyncStorageOptions = {},
): AsyncStorage {
  const store = opts.store ?? getDefaultStore()

  return {
    async getItem(key: string): Promise<string | null> {
      // idb-keyval `get`은 unknown 반환 — string 검증 후 fallthrough.
      const raw = await runStoreGet(store, key)
      if (raw === undefined || raw === null) {
        return null
      }
      // persister는 항상 string으로 set하므로 string 외 값은 데이터 손상 신호 →
      // 안전하게 null로 폴백(throw하면 persister가 hydrate 전 crash).
      if (typeof raw !== 'string') {
        return null
      }
      return raw
    },
    async setItem(key: string, value: string): Promise<void> {
      await runStoreSet(store, key, value)
    },
    async removeItem(key: string): Promise<void> {
      await runStoreDel(store, key)
    },
  }
}

// idb-keyval의 함수형 API와 mock 객체형 store 둘 다 지원하기 위한 디스패치 헬퍼.
// `UseStore`는 idb-keyval 내부에서 옵션으로 전달되는 callback 형태이므로
// 형 판별 후 적절히 호출.
function isMockStore(
  store: IdbStore,
): store is {
  get(key: string): Promise<unknown>
  set(key: string, value: unknown): Promise<void>
  del(key: string): Promise<void>
} {
  // UseStore는 함수가 아니라 객체이지만, 내부 메서드는 노출되지 않음. 우리
  // mock은 명시적 `get`/`set`/`del` 메서드를 제공.
  return (
    typeof (store as { get?: unknown }).get === 'function' &&
    typeof (store as { set?: unknown }).set === 'function' &&
    typeof (store as { del?: unknown }).del === 'function'
  )
}

async function runStoreGet(store: IdbStore, key: string): Promise<unknown> {
  if (isMockStore(store)) {
    return store.get(key)
  }
  return get(key, store)
}

async function runStoreSet(
  store: IdbStore,
  key: string,
  value: string,
): Promise<void> {
  if (isMockStore(store)) {
    await store.set(key, value)
    return
  }
  await set(key, value, store)
}

async function runStoreDel(store: IdbStore, key: string): Promise<void> {
  if (isMockStore(store)) {
    await store.del(key)
    return
  }
  await del(key, store)
}

/**
 * tenant 별 persist key 헬퍼 (D-PWAPER-2 권장 default — tenant 별 key 접두).
 *
 * - `persistKeyForTenant('t1')` → `'rosshield-rq-t1'`
 * - `persistKeyForTenant()` → `'rosshield-rq-anon'` (로그인 전 또는 tenantId 없음)
 *
 * Stage 2에서 `App.tsx`가 `useAuthStore` tenantId를 구독해 이 헬퍼로 key를
 * 결정 — tenant 변경 시 새 persister(useMemo 의존성)로 자동 namespace 분리.
 */
export function persistKeyForTenant(
  tenantId?: string | null,
): string {
  if (tenantId === undefined || tenantId === null || tenantId === '') {
    return 'rosshield-rq-anon'
  }
  return `rosshield-rq-${tenantId}`
}
