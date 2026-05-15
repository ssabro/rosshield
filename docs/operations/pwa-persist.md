# rosshield PWA Persist (IndexedDB 캐시) 운영 가이드 (PWA persist epic 4/4 마감)

> **상태**: PWA persist Stage 1~4 (idb-storage + persister + dehydrate filter +
> logout clear + 운영자 docs) 머지 완료. PWA persist epic 4/4 stage 마감.
> **참조**: `docs/design/notes/pwa-persist-design.md` (epic 본문 + D-PWAPER-1~7
> 결정 로그). 직전 PWA epic 운영자 docs는 `docs/operations/pwa-offline.md`.
> **대상**: 어플라이언스 운영자 / 감사인 / 데모 진행자 / 보안 검토자.

본 문서는 rosshield Web Console의 **react-query persist + IndexedDB** 동작을
운영하는 가이드입니다. 직전 PWA epic의 셸/메뉴/install/mutation 가드 위에 더해
**백엔드 단절 + 페이지 reload 후에도 마지막 read 데이터를 IndexedDB에서
hydrate**해 read-only 진입을 가능하게 합니다.

---

## 1. 동작 원리

### 1.1 구성 요소 4종

design doc §6.1 outline을 따른 모듈 4종이 결선되어 있습니다:

| 영역 | 파일 | 책임 |
|---|---|---|
| AsyncStorage 어댑터 | `web/src/lib/persist/idb-storage.ts` | `idb-keyval` 위 IndexedDB get/set/del wrap. tenant 별 key 헬퍼 (`persistKeyForTenant`). |
| persister | `web/src/lib/persist/persister.ts` | `@tanstack/query-async-storage-persister` 팩토리 + `PERSIST_OPTIONS_BASE` (maxAge 7일 + dehydrate filter). |
| dehydrate filter | `web/src/lib/persist/dehydrate-filter.ts` | `shouldDehydrateQuery` 콜백 + `DENY_KEY_PREFIXES` (sso/webhooks/invitations/advisor 4종). |
| logout clear | `web/src/lib/persist/clear.ts` | `clearPersistedTenant(tenantId)` + `runLogoutClear({ queryClient, tenantId, clearSession })` 헬퍼. |
| Provider 결선 | `web/src/App.tsx` | `QueryClientProvider` → `PersistQueryClientProvider` 교체 + `useAuthStore` tenantId 구독. |

### 1.2 hydrate / dehydrate 라이프사이클

1. **첫 진입** — `App.tsx` 마운트 시 `PersistQueryClientProvider`가 IndexedDB에서
   tenant key(`rosshield-rq-${tenantId}` 또는 `rosshield-rq-anon`)의 직렬화된
   `PersistedClient`를 비동기 hydrate. 캐시 없음 또는 7일 초과 시 빈 상태.
2. **사용 중** — react-query가 useQuery 결과를 메모리에 보관. 1초 throttle로
   IndexedDB에 자동 dehydrate(`@tanstack/query-async-storage-persister` default).
3. **dehydrate 차단** — `shouldDehydrateQuery`가 false 반환 시(deny list 매치)
   해당 query는 IndexedDB 영속 안 함 → 메모리만 보관.
4. **다음 진입(reload 또는 재방문)** — IndexedDB hydrate → 즉시 마지막 데이터로
   UI 표시 → 백엔드 응답 도착 시 자동 갱신(stale-while-revalidate).

### 1.3 maxAge 7일 (D-PWAPER-3)

- `PERSIST_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000` (`web/src/lib/persist/persister.ts`).
- hydrate 시점에 캐시 timestamp가 7일 초과면 자동 무시 → 빈 상태로 진입.
- 어플라이언스 점검 cycle(주 1회)와 정합 — 일주일 미운영 후 재진입 시 자동
  갱신.

---

## 2. Multi-tenant 격리

### 2.1 tenant 별 storage key (D-PWAPER-2)

- `persistKeyForTenant(tenantId)` 헬퍼:
  - `'t1'` → `'rosshield-rq-t1'`
  - `null` / `undefined` / `''` → `'rosshield-rq-anon'` (로그인 전 또는 tenantId
    없음)
- `App.tsx`가 `useAuthStore.user?.tenantId`를 구독해 `useMemo` 의존성으로 새
  persister 인스턴스 자동 생성 → tenant 변경 시 namespace 자동 분리.
- **단일 origin SSO 다중 tenant** 시나리오에서 사용자 A 캐시 ↔ 사용자 B 로그인
  누설 사전 차단. 다른 tenant 캐시는 같은 IndexedDB DB 내 다른 key로 격리되며
  명시 clear 없이도 hydrate 시 무시됩니다.

### 2.2 logout 시 clear 동작 (D-PWAPER-4)

`web/src/api/hooks.ts::useLogout`이 logout API 응답 후 `runLogoutClear`로 3단계
clear를 실행합니다:

1. **`queryClient.clear()`** — 메모리 캐시 즉시 비움(다음 렌더 빈 상태).
2. **`clearPersistedTenant(tenantId)`** — IndexedDB의 해당 tenant key 제거.
3. **`clearSession()`** — accessToken + user 비움 → router가 로그인 페이지로
   redirect.

`web/src/api/client.ts`의 401 + refresh 실패 site에서도 동일 호출
(`clearSessionWithPersist`). queryClient는 본 모듈에서 접근 불가(Provider
외부)이므로 메모리 clear는 useLogout이 담당하고, client.ts는 IndexedDB +
session만 clear.

### 2.3 tenant 전환은 명시 clear 불필요

- D-PWAPER-2 tenant 별 key로 자동 격리됨 → 별 tenant로 전환 시 새 persister가
  새 key namespace를 hydrate, 기존 tenant key는 IndexedDB에 남아 있되 maxAge
  7일에 자연 만료(orphan).

---

## 3. 보안 — 민감 데이터 차단 list (D-PWAPER-5)

### 3.1 정책: opt-out (allow by default + deny list)

`web/src/lib/persist/dehydrate-filter.ts`의 `DENY_KEY_PREFIXES`:

| queryKey prefix | 차단 사유 |
|---|---|
| `sso` | OIDC clientSecret(서버 redact 가정이나 응답 형식 변경 시 위험) |
| `webhooks` | webhook signing secret(redact 가정), URL은 내부 endpoint |
| `invitations` | invitation token(URL 활성 시 가입 권한 부여) |
| `advisor` | LLM 대화 사용자 입력 민감 가능 |

**매치 정책**: `queryKey[0]` 정확 일치 — `startsWith` 아님. `'sso-config'` 같은
별도 prefix는 통과. 위치도 정확 — `queryKey[0]`만 검사.

### 3.2 deny list 갱신 절차

새 hook 추가 시 보안 검토 후 deny list 갱신 — 개발자 부담:

1. `web/src/lib/persist/dehydrate-filter.ts`의 `DENY_KEY_PREFIXES`에 새 prefix
   추가(`Object.freeze` 배열).
2. `web/src/lib/persist/dehydrate-filter.test.ts` 단위 테스트 갱신(deny 매치
   1건 + allow 통과 1건).
3. 본 문서 §3.1 표 갱신.
4. 기존 사용자의 IndexedDB에는 차단 직전 캐시가 남을 수 있음 — release note에
   "DevTools Application > IndexedDB > rosshield-rq 삭제 권장" 안내.

### 3.3 의도적 한계

- **mutation 결과 persist 안 함** — react-query 기본은 mutation 캐시 dehydrate
  안 함. `shouldDehydrateMutation` 미설정. audit chain leader epoch 정합성
  위험 회피.
- **WebSocket persist 안 함** — `useScanProgress`(`web/src/api/hooks.ts:1241`)는
  react-query 외부. SW와 직교, persist도 직교.
- **암호화 미실행** — IndexedDB 평문. crypto.subtle 적용은 별 epic(key 관리 +
  성능 영향 분석 필요). 디바이스 탈취 시 read 가능 → mitigation: logout 시
  자동 clear + 24h refresh token 만료(서버 측).

---

## 4. 트러블슈팅

### 4.1 IndexedDB 상태 확인 (DevTools)

1. F12 → **Application** 패널.
2. 좌측 **Storage** 트리에서 **IndexedDB** 펼치기.
3. **`rosshield-rq`** DB → **`keyval`** object store 클릭.
4. key 컬럼에서 `rosshield-rq-${tenantId}` 또는 `rosshield-rq-anon` 확인.
5. value 컬럼은 직렬화된 `PersistedClient` JSON — `clientState.queries` 배열
   안에 `queryKey` + `state.data` + `state.dataUpdatedAt` 확인.

### 4.2 "오프라인 reload 후 화면이 빈 데이터" — 호소

**원인 후보**:

1. **persist 자체가 비활성화** — 사용자 환경에서 `App.tsx`가 `PersistQueryClientProvider`
   미사용(쓰지 않는 빌드). 빌드 산출물 검증.
2. **첫 hydrate 전 reload** — 아직 IndexedDB 첫 dehydrate 전(첫 진입 후 1초 미만)에
   reload하면 캐시 비어있음. throttleTime 1초 default — 정상 동작.
3. **maxAge 7일 초과** — 마지막 hydrate가 7일 전이면 자동 무시. DevTools에서
   `dataUpdatedAt` 확인.
4. **deny list 매치** — 해당 query가 sso/webhooks/invitations/advisor 중 하나면
   영속 안 함. 다른 query에서 검증.

**해결**: §4.1 절차로 IndexedDB 상태 확인 후, 정상 데이터가 있으면 새로고침
시점의 hydrate timing 이슈 가능성 → 별 페이지 진입 후 1~2초 대기 후 reload.

### 4.3 "quota exceeded" 에러 호소

**원인**: scan 결과 누적이 IndexedDB origin quota(Chrome ~60% 디스크 / Firefox
~50% / Safari ~1GB) 초과. 어플라이언스 NUC(SSD 256GB+)에서는 대단히 드뭅니다.

**임시 해결**:

1. §4.1 절차로 IndexedDB > `rosshield-rq` DB 우클릭 → **Delete database**.
2. 사용자에게 "다음 진입 시 캐시 다시 채워집니다" 안내 — 신규 데이터는 백엔드
   재fetch.
3. 또는 사용자가 logout → 재로그인 시 자동 clear(D-PWAPER-4).

**근본 해결**: 후속 epic의 `navigator.storage.estimate()` 모니터(80% 도달 시
자동 prune) — 본 epic 비대상(design doc §7 Stage 4 후속 옵션).

### 4.4 "logout 후에도 IndexedDB에 데이터가 남음"

**원인 후보**:

1. **IndexedDB clear 시점의 throw** — 권한/quota 등 이유로 throw 시 useLogout은
   메모리 + session은 항상 clear하지만 IndexedDB는 부분 잔존 가능. 콘솔에
   `[rosshield] persist clear 실패 (logout 진행)` warn 노출.
2. **다른 tenant key 잔존** — D-PWAPER-2 정책상 logout은 현재 tenant key만 clear.
   다른 tenant 캐시는 maxAge 7일에 자연 만료.

**수동 clear**: §4.1 절차 또는 DevTools > Application > **Storage** > **Clear
site data** (`rosshield-auth` zustand persist localStorage도 비워지므로 재로그인
필요).

### 4.5 "stale data 표시 — 며칠 전 데이터로 보임"

**원인**: maxAge 7일 이내 캐시 + 백엔드 응답 미도착 상태(오프라인 또는
백엔드 단절).

**확인**: §4.1 절차로 `dataUpdatedAt` 확인. 사용자에게 OfflineIndicator banner
(직전 PWA epic Stage 3) 노출 여부 확인. 본 epic 비대상이지만 후속 epic의
StaleDataBadge UX(D-PWAPER-7 권장 default = enable)로 "마지막 갱신 X분 전"
표시 예정.

---

## 5. 비활성화 절차

운영자 또는 보안 검토자가 IndexedDB persist를 끄고 싶을 때.

### 5.1 사용자 측 일시 비활성화 (DevTools 1회)

1. §4.1 절차로 IndexedDB > `rosshield-rq` DB 우클릭 → **Delete database**.
2. 다음 진입 시 빈 캐시로 시작. 단, 다음 dehydrate에서 다시 채워짐.

### 5.2 빌드 측 영구 비활성화 (별 빌드 산출)

본 epic은 환경 변수 또는 build flag로 persist 자체를 toggle하는 옵션을 별도
구현하지 않았습니다. 영구 비활성화가 필요한 customer 시나리오 시:

1. `web/src/App.tsx`의 `PersistQueryClientProvider`를 표준 `QueryClientProvider`로
   되돌립니다. import + JSX 두 곳 수정 — ~5줄.
2. 별 빌드 산출(`pnpm build`)로 binary 교체.
3. 사용자에게 "기존 IndexedDB는 자동 정리되지 않으므로 §5.1 절차로 수동 삭제
   권장" 안내.

후속 epic 후보: `import.meta.env.VITE_DISABLE_PERSIST === 'true'`일 때
`createPersister`가 in-memory no-op persister를 반환하도록 분기. 본 epic
비대상.

### 5.3 부분 비활성화 (특정 query만)

- `useQuery` 옵션의 `meta` 또는 별 `gcTime: 0` 설정으로 메모리 + 영속 모두 회피.
- 또는 dehydrate filter의 deny list에 해당 prefix 추가(§3.2 절차).

---

## 6. 의도적 한계 (비목표)

design doc §9.7 "한계 (의도적)" 정렬:

- **mutation 오프라인 queueing 미실행** — 직전 PWA epic의 button-level disabled
  + 본 epic의 mutation 결과 비영속 정합. audit chain leader epoch 정합성
  위험으로 비목표.
- **암호화 미실행** — IndexedDB 평문. 별 epic 주제(crypto.subtle + key 관리).
- **multi-tab sync 미실행** — 동일 사용자 다중 탭에서 logout 시 다른 탭은 stale
  가능. BroadcastChannel은 후속 epic.
- **Tauri 데스크톱 직교** — Tauri는 별 storage(SQLite plugin) 사용 가능 → 본
  epic은 web 브라우저 환경만.
- **stale data badge UX 미실행** — D-PWAPER-7 권장 default = enable이나 본
  epic은 read 캐시 결선까지만. 후속 epic에서 `dataUpdatedAt` 기반 "n분 전"
  표시 예정.

---

## 참조

- `docs/design/notes/pwa-persist-design.md` — PWA persist epic 본문 design doc
  + D-PWAPER-1~7 결정 로그 + Stage 분해.
- `docs/operations/pwa-offline.md` — 직전 PWA epic 운영자 docs (manifest + SW +
  offline UX + mutation 가드).
- `docs/design/01-principles.md` §3 (에어갭 1급) / §10 (프라이버시 — 로컬 우선)
  / §11 (설명 가능성) / §12 (점진적 적용).
- `docs/design/04-domain-and-data-model.md` — multi-tenancy tenant_id scope
  일관 (D-PWAPER-2).
- `docs/design/06-security-and-tenancy.md` — 민감 데이터 차단 list(D-PWAPER-5)
  근거.
- `@tanstack/react-query-persist-client`:
  <https://tanstack.com/query/latest/docs/framework/react/plugins/persistQueryClient>.
- `@tanstack/query-async-storage-persister`:
  <https://tanstack.com/query/latest/docs/framework/react/plugins/createAsyncStoragePersister>.
- `idb-keyval`: <https://github.com/jakearchibald/idb-keyval>.
