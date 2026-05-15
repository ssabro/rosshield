# PWA Persist (react-query persist + IndexedDB) — 옵션 C trigger design doc

> **상태**: PWA epic Stage 1~4 마감 직후(head `76ae2f0`, `feat(web): PWA Stage 4 — mutation 가드 + 운영자 docs (PWA epic 마감)`). 본 문서는 **코드 0줄 / 마이그레이션 0** — `pwa-offline-design.md` §3.4 + §4.3 + §7 Stage 6에서 trigger로 명시한 "react-query persist" 진입을 위한 별 design doc 입니다.
> **참조**: 직전 epic `docs/design/notes/pwa-offline-design.md` (553줄, 옵션 A 채택 마감) + memory `feedback_design_doc_first.md` (큰 작업 design doc 우선) + memory `feedback_design_doc_conservative.md` (추정 보수적).
> **R 식별자**: 별도 R 미할당 (Phase 5 backlog 후속 후보 — `phase5-backlog.md`에 "PWA persist (react-query + IndexedDB)" 별 라인으로 등재 예정).

---

## 1. 상태 / 배경

### 1.1 PWA epic 4/4 마감 직후 시점

직전 PWA epic(`pwa-offline-design.md`)이 4 stage 모두 마감되었습니다:

- **Stage 1**: manifest.webmanifest + 아이콘(192·512·apple-touch·favicon) + index.html link/theme-color (`web/index.html`).
- **Stage 2**: vite-plugin-pwa generateSW + Workbox precache(globPatterns) + navigateFallback + navigateFallbackDenylist `[/^\/api\//]` (`web/vite.config.ts`).
- **Stage 3**: `web/src/components/OfflineIndicator.tsx` + `UpdatePrompt.tsx` + `web/src/lib/use-is-offline.ts` + `pwa-update.ts` + i18n 4 키.
- **Stage 4**: button-level mutation 가드(`mutationGuardTitle` 헬퍼 + 7 페이지에 `disabled={offline}` 결선) + `docs/operations/pwa-offline.md` 운영자 docs.

이 결과 **셸 + 메뉴 + 라우터 + 마지막 페이지 진입**까지는 백엔드 단절에도 충족되나, **데이터(react-query 캐시)는 페이지 새로고침 한 번에 즉시 휘발**합니다. 운영자가 어플라이언스 점검 중 노트북 일시 단절 + 의도치 않은 새로고침 시 메뉴는 보이나 dashboard 카드 / robot 목록 / scan 결과 0개로 표시됩니다.

### 1.2 옵션 C trigger 명시 위치

`pwa-offline-design.md` §3.4 "에어갭 시나리오 정의" 표:

| 시나리오 | 기대 동작 | 옵션 A 충족 | 옵션 C 충족 |
|---|---|---|---|
| 인증 후 사용 중 백엔드 단절 | 메뉴 + 마지막 페이지 데이터 read OK | △ (메뉴만) | ✅ (read-only) |

및 §7 Stage 6(옵션 C 진입 trigger)에 명시:

> trigger 시나리오:
> - 첫 paying customer가 "오프라인 read 필수"라고 명시한 경우.
> - 또는 어플라이언스 PoC 30일 운영 중 read 캐시 부재로 사용성 이슈 보고된 경우.

본 design doc은 trigger가 아직 발화하지 않은 상태에서 **사전 옵션 비교 + Stage 분해 + 결정 항목 권장 default**까지만 마감해, trigger 발화 시 즉시 진입할 수 있도록 준비합니다.

### 1.3 가치 / 위험 요약

**가치**:
1. **에어갭 §3 강화** — 백엔드 단절에도 마지막 read 데이터로 read-only 모드 진입.
2. **새로고침 보호** — 사용자가 의도치 않게 F5/Ctrl-R한 경우에도 데이터 손실 0.
3. **모바일 install 시 첫 진입 UX ↑** — install 후 재진입 시 즉시 데이터 표시(network 대기 없음, then revalidate).

**위험 (보수적)**:
1. **민감 데이터 영속화** — license token / SSO clientSecret / webhook secret / advisor 대화가 IndexedDB에 평문 저장 시 XSS 또는 디바이스 탈취 시 노출.
2. **multi-tenant 캐시 누수** — 사용자 A의 캐시가 사용자 B 로그인 시 표시 위험(SSO 시나리오 + 공용 디바이스).
3. **stale data 신뢰 손상** — 마지막 캐시가 며칠 전 상태인데 indicator 없이 표시되면 사용자가 최신으로 오인 + 잘못된 audit 판단.
4. **Storage quota 초과** — IndexedDB는 origin 단위 ~50MB+ 가능하나, 대량 scan 결과 누적 시 quota exceeded 오류 가능.

### 1.4 추정 시간 / 잠재 효과 (보수적)

- **추정 시간**: 옵션 A(localStorage persister) 1.0~1.5일 / 옵션 B(IndexedDB persister) 1.5~2.5일 / 옵션 C(수동 IndexedDB) 2.5~3.5일.
- **잠재 효과**: 직접 매출 0. 첫 paying customer 데모/PoC 시나리오에서 단절 신뢰성 ↑. 어플라이언스 30일 PoC 운영 incident 회피.
- **회귀 위험**: 중상 — multi-tenant 격리 + 민감 데이터 차단 + stale 정책 동시 만족 필요.

---

## 2. 현재 상태 진단

### 2.1 react-query 사용 현황

`web/src/api/hooks.ts` (2181줄, 74개 useQuery/useMutation) 상태:

| 항목 | 값 | 출처 |
|---|---|---|
| 클라이언트 | `@tanstack/react-query@^5.95.0` | `web/package.json:36` |
| QueryClient 생성 | `web/src/App.tsx:24-34` (defaultOptions: staleTime 30s, refetchOnWindowFocus false) | `web/src/App.tsx` |
| persistor | **부재** — 메모리 only | (전 grep) |
| useQuery 수 | ~30개 (queryKey grep 30+) | `hooks.ts` queryKey 위치 |
| useMutation 수 | ~24개 (RBAC Stage 4 매트릭스 동수) | RBAC Stage 4 commit |
| 평균 staleTime | 30s default + 일부 30s polling(`useBackups`, `useUsageStats`) | `hooks.ts:620, 650` |
| WebSocket 사용 | 1곳(`useScanProgress`) — react-query 외부 | `hooks.ts:1241` |

### 2.2 zustand persist 현황

- `web/src/stores/auth.ts` — `persist` middleware + `createJSONStorage(() => localStorage)` + key `rosshield-auth` + partialize: `{ accessToken, user }`.
- `web/src/stores/theme.ts` — 별 store, 동일 패턴.
- **clearSession 호출 위치**: `auth.ts:52` + `client.ts:71/77/82` (401 + refresh 실패) + `useLogout`(`hooks.ts:73-89`) + 로그인 화면 transition.

### 2.3 IndexedDB 부재

- `navigator.indexedDB` 직접 호출 0 (전 web grep).
- `idb` / `idb-keyval` / `dexie` devDep 0건 (`package.json` devDep 76줄 grep 0건).
- `@tanstack/query-sync-storage-persister` / `@tanstack/query-async-storage-persister` / `@tanstack/react-query-persist-client` 모두 미설치.

### 2.4 옵션 C trigger 정의 (직전 epic §3.4 + §7 발췌)

trigger는 다음 둘 중 하나 발화 시 진입:

1. **첫 paying customer 명시 요구** — "오프라인 read 필수".
2. **어플라이언스 PoC 30일 운영 중 read 캐시 부재로 사용성 이슈 보고**.

trigger 발화 전이라도 본 design doc은 `feedback_design_doc_first.md` 정책에 따라 코드 진입 부담 0의 사전 정리 산출물로 작성합니다.

### 2.5 민감 데이터 후보 식별 (hooks.ts grep)

다음 query는 평문 캐시 시 보안 영향 가능 — **persist 차단 후보**:

| 영역 | hook | queryKey | 민감도 | 사유 |
|---|---|---|---|---|
| License | `useLicenseInfo` | `['license', 'info']` | 중 | 만료일·feature flag·quota — 누설 시 정책 추론 가능 |
| SSO | `useSsoProviders` / `useSsoProvider` | `['sso', 'providers', ...]` | **상** | OIDC clientSecret(서버 redact 가정이나 응답 형식 변경 시 위험) |
| Webhooks | `useWebhookEndpoints` | `['webhooks']` | **상** | webhook signing secret(redact 가정), URL은 내부 endpoint |
| Invitations | `useInvitations` / `useInvitationByToken` | `['invitations', ...]` | **상** | 초대 token(URL 활성 시 가입 권한) |
| Advisor | `useAdvisorConversations` / `useAdvisorConversation` | `['advisor', ...]` | 중 | LLM 대화 — 사용자 질문에 민감 정보 포함 가능 |
| Audit head | `useAuditHead` | `['audit', 'head']` | 저 | hash chain head — 공개 metadata 수준 |
| Robots / Scans | `useRobots` / `useScans` 등 | `['robots'...]`, `['scans'...]` | 저 | 운영 데이터 — 본 epic 핵심 가치 대상 |

**핵심**: SSO clientSecret + webhook secret + invitation token은 **반드시 차단 list에 포함** (D-PWAPER-5).

### 2.6 인증 토큰 + persist 상호작용

- accessToken은 zustand persist로 이미 localStorage 평문(C6 결정).
- react-query persist가 IndexedDB에 추가되어도 accessToken 자체는 zustand 영역(중복 금지) — request header로만 부착.
- **위험 시나리오**: 만료 accessToken으로 stale read 캐시 표시 → 사용자가 최신으로 오인 → 단절 회복 후 mutation 시도 → 401 → 로그아웃. **mitigation**: stale indicator + maxAge(D-PWAPER-3).

---

## 3. 요구 사항 분류

### 3.1 persist 범위

- **read 쿼리만 영속**: useQuery 결과만. useMutation 결과는 메모리만(데이터 정합성 + audit chain leader epoch 위험 — `pwa-offline-design.md` §3.5 비요구 정렬).
- 분리 방법: `persistQueryClient` 옵션의 `dehydrateOptions.shouldDehydrateQuery`로 mutation 캐시 제외(react-query는 mutation도 별 캐시 가능).

### 3.2 persistor 선택

- **localStorage (5MB 한계)**: 단순. JSON.stringify 비용 큼. 동기 — 메인 스레드 차단.
- **IndexedDB (50MB+ 권장 quota)**: async. idb-keyval 어댑터 또는 `@tanstack/query-async-storage-persister`. 메인 스레드 비차단.
- **권장**: IndexedDB (D-PWAPER-1) — scan 결과 누적 시 5MB 한계 부족 가능, react-query 표준 패키지 존재.

### 3.3 multi-tenant 격리

- **storage key tenant 접두**: key를 `rosshield-rq-${tenantId}` 형식으로 분리해 tenant 전환 시 별 namespace.
- **logout 시 clear 필수**: `useLogout` + `clearSession` 호출 site에서 `persistor.removeClient()` + `queryClient.clear()` 동시.
- **tenant 전환 시 invalidate**: 단일 origin(SSO 다중 tenant) 시나리오에서 tenant 변경 감지 시 캐시 전체 invalidate.

### 3.4 stale 정책

- **maxAge (전역)**: 7일(D-PWAPER-3 default). 7일 초과 캐시는 hydrate 시 자동 무시.
- **buster (앱 버전 기반)**: build hash 또는 `__APP_VERSION__` 환경 변수로 buster 설정 → 새 deploy 시 캐시 자동 무효화.
- **query별 staleTime**: 기존 30s default 유지. persist 캐시는 hydrate 후 즉시 stale → 백엔드 응답 도착 시 자동 갱신(stale-while-revalidate).
- **stale indicator UX**: hydrate된 데이터는 cached-at timestamp 보유 → UI에서 "마지막 갱신 X분 전" badge 노출(D-PWAPER-7).

### 3.5 보안 — 민감 데이터 차단 list

`shouldDehydrateQuery` 콜백으로 다음 queryKey prefix는 영속 차단(D-PWAPER-5 default):

```
['sso']           — clientSecret 누설 위험
['webhooks']      — signing secret 누설 위험
['invitations']   — invitation token 누설 위험
['advisor']       — LLM 대화 사용자 입력 민감 가능
```

**기본 정책**: opt-out 방식(allow by default + deny list). 새 hook 추가 시 보안 검토 후 deny list 갱신 — 개발자 부담.

**대안**: opt-in 방식(deny by default + allow list) — 새 hook이 자동으로 영속 안 되어 안전하나, allow list 누락 시 cache 가치 0(보수적).

### 3.6 회귀 격리

- 기존 zustand persist(`rosshield-auth`)는 **완전 분리 storage** (localStorage vs IndexedDB) — 영향 0.
- 기존 SW(vite-plugin-pwa Stage 2)는 **API endpoint 캐시 안 함**(navigateFallbackDenylist `[/^\/api\//]`) — react-query persist와 직교.
- 기존 `useIsOffline`(Stage 3)와 mutation 가드(Stage 4)는 그대로 — read 캐시 hydrate는 offline 무관.

---

## 4. 합성 전략 옵션 (≥3)

### 4.1 옵션 A — `@tanstack/query-sync-storage-persister` + localStorage

**범위**: TanStack Query 공식 sync persister + localStorage. `persistQueryClient` HOC 또는 `PersistQueryClientProvider` 컴포넌트로 결선.

**Pros**:
- 가장 단순 — devDep 2개(`@tanstack/query-sync-storage-persister` + `@tanstack/react-query-persist-client`), 구성 ~20줄.
- `App.tsx` `QueryClientProvider`를 `PersistQueryClientProvider`로 1줄 교체 + persister + buster 설정만.
- 동기 hydrate — 첫 렌더에 캐시 즉시 사용 가능(loading flash 없음).

**Cons**:
- **localStorage 5MB 한계** — 대량 scan 결과(robot 100대 × scan 10회 × finding 50개) 누적 시 quota exceeded 가능.
- **동기 — 메인 스레드 차단** — 큰 캐시 hydrate/serialize 시 첫 페인트 지연(체감 100~300ms).
- localStorage는 origin 단위 — multi-tab race condition 시 마지막 write win.

**회귀 위험**: 중. quota exceeded 오류는 silent failure(throw 처리 누락 시 캐시 부분 손실).

**코드 변경 추정**: `web/package.json` devDep +2 / `web/src/App.tsx` ~30줄 / `web/src/lib/persist-query.ts` 신규 ~50줄(buster + maxAge + dehydrate filter) / 단위 테스트 ~80줄. 총 ~160줄.

**운영 영향**: 사용자 localStorage `rosshield-rq` key 점유 ~수 MB. logout 시 `localStorage.removeItem('rosshield-rq')` + `queryClient.clear()`.

### 4.2 옵션 B — `@tanstack/query-async-storage-persister` + IndexedDB (idb-keyval 어댑터)

**범위**: TanStack Query 공식 async persister + idb-keyval(또는 동등) IndexedDB key-value 어댑터. async hydrate.

**Pros**:
- **IndexedDB 50MB+ quota** — scan 결과 누적 충분.
- **async — 메인 스레드 비차단** — 큰 캐시도 첫 페인트 영향 없음.
- multi-tab safe — IndexedDB transaction 모델로 race condition 회피.
- TanStack Query 공식 패턴 — Workbox + react-query 조합 표준.

**Cons**:
- **첫 hydrate flash** — async라 첫 렌더에 캐시 비어있음 → 200~500ms 후 hydrate 완료 → re-render. 사용자 체감 짧은 깜빡임.
- devDep +3(`@tanstack/query-async-storage-persister` + `@tanstack/react-query-persist-client` + `idb-keyval` 또는 `idb`).
- 디버깅 복잡 — DevTools Application > IndexedDB > rosshield-rq 직접 확인 절차 사용자 안내 필요.

**회귀 위험**: 중. async hydrate 중 사용자가 빠르게 mutation 시도 시 race(권장 mitigation: hydrate 완료 전 `<Suspense>` fallback).

**코드 변경 추정**: `web/package.json` devDep +3 / `web/src/App.tsx` ~40줄 / `web/src/lib/persist-query.ts` 신규 ~80줄(idb-keyval wrap + buster + maxAge + dehydrate filter + tenant key) / 단위 테스트 ~120줄(idb-keyval mock 포함). 총 ~240줄.

**운영 영향**: 사용자 IndexedDB `rosshield-rq` DB 점유 ~수십 MB. logout 시 `idbKeyval.del(key)` + `queryClient.clear()`. 첫 렌더 hydrate flash 대응 UX 필요.

### 4.3 옵션 C — 수동 IndexedDB 어댑터 (`@tanstack/react-query-persist-client`만 + 자체 storage)

**범위**: `@tanstack/react-query-persist-client` 공식 + idb-keyval 같은 외부 dep 없이 자체 IndexedDB CRUD wrapper(~120줄) 작성.

**Pros**:
- 외부 dep 최소(공식 1개만).
- IndexedDB 동작 100% 통제(tenant scope DB 분리, transaction 명시 제어).
- 보안 리뷰 단순 — 외부 dep 0의 wrapper 코드만 검증.

**Cons**:
- **작성 비용 ↑** — IndexedDB raw API는 callback 기반 + edge case(versionchange, blocked) 多 → wrapper ~120줄 + 테스트 ~150줄.
- 회귀 디버깅 부담 — wrapper 자체 버그 시 react-query persist 신뢰성 손상.
- idb-keyval(잘 검증된 ~3KB 라이브러리) 회피의 가치는 본 epic 단독으로 정당화 약함.

**회귀 위험**: 중상. 자체 코드 = 모든 edge case 자체 책임.

**코드 변경 추정**: `web/package.json` devDep +1(`@tanstack/react-query-persist-client`만) / `web/src/lib/idb-storage.ts` 신규 ~120줄 / `web/src/lib/persist-query.ts` 신규 ~80줄 / 단위 테스트 ~200줄(IndexedDB mock 직접 작성). 총 ~400줄.

**운영 영향**: 옵션 B와 동일. 사용자 영향 무차별.

### 4.4 옵션 D — 보류 (현 단계 paying customer 0이라 ROI 미미)

**범위**: 본 design doc만 마감 후 진입 보류. trigger 발화(첫 paying customer 명시 요구 또는 어플라이언스 PoC 30일 운영 보고) 전까지 코드 0.

**Pros**:
- ROI 명확화 후 진입 → 사후 후회 0.
- 본 design doc만으로 trigger 발화 시 즉시 진입 가능(0일 사전 준비).
- 보안 위험(민감 데이터 영속) 사전 회피 — 실제 customer 환경 검토 후 정책 확정.

**Cons**:
- 직전 PWA epic의 §3.4 옵션 C 충족 시나리오 미해결 — "인증 후 사용 중 백엔드 단절 → 데이터 read OK" 부재.
- 운영자 PoC 중 사용성 이슈 보고 시 2~3일 대응 lead time 발생.

**회귀 위험**: 0.

**코드 변경 추정**: 0줄.

**운영 영향**: 0. trigger 발화 시 옵션 B 또는 옵션 A 즉시 진입.

---

## 5. 권장 옵션 + 근거

**1차 권장 default: 옵션 D (보류)** — trigger 발화 전까지 코드 진입 보류.

**근거**:

1. **ROI 미확정** — 첫 paying customer 0 + 어플라이언스 PoC 미운영 시점에서, 옵션 C의 가치는 가설 단계. 옵션 D는 가설 검증 후 실제 정책 확정 가능.
2. **보안 위험 사전 회피** — 민감 데이터 차단 list(D-PWAPER-5)는 customer 환경(SSO provider 종류·webhook 사용 패턴·license token 형식)에 따라 정책 변경 가능. 사전 영속화 후 차단 정책 변경은 기존 캐시 마이그레이션 부담.
3. **본 design doc의 가치 충족** — trigger 발화 시 즉시 진입 가능한 옵션 비교 + Stage 분해 + D-PWAPER-1~7 default 까지 마감 → "준비된 상태에서 보류"가 "급하게 진입"보다 안전.
4. **기존 PWA epic Stage 1~4의 가치는 그대로** — 셸/메뉴/install/mutation 가드는 이미 충족. 옵션 C는 가치의 한 단계 증분.

**2차 권장 default (trigger 발화 시): 옵션 B — async storage + IndexedDB**.

**근거**:

1. **에어갭 §3 가치 핵심 충족** — IndexedDB 50MB+ quota는 scan 결과 누적에 충분. localStorage 5MB는 어플라이언스 PoC 30일 시나리오에서 quota exceeded 위험.
2. **메인 스레드 비차단** — 옵션 A의 동기 hydrate는 큰 캐시 시 첫 페인트 지연 → UX 손상. async는 짧은 깜빡임 대신 첫 페인트 즉시.
3. **공식 + 검증된 dep 조합** — TanStack Query async persister + idb-keyval은 광범위 사용. 옵션 C(수동 wrapper)는 회귀 위험 폭탄 대비 가치 부족.
4. **점진 적용(P12) 정합** — 옵션 A → 옵션 B 마이그레이션은 storage 형식 변경 = 사용자 캐시 1회 손실. 옵션 B 처음부터가 후속 변경 0.

**기각 근거 요약**:
- 옵션 A: localStorage 5MB는 어플라이언스 PoC 30일 시나리오에서 부족 가능. 옵션 B 대비 가치 부족.
- 옵션 C: idb-keyval 회피의 가치는 본 epic 단독으로 정당화 약함. 회귀 위험 ↑.

**대안 default**: 사용자가 "보안 정책상 IndexedDB 절대 금지" 명시 시 → **옵션 D(보류) 무기한**. 사용자가 "지금 즉시 가치 검증" 명시 시 → 옵션 B 진입.

---

## 6. 변경 사항 outline (옵션 B 채택 시)

### 6.1 신규 / 수정 파일 (정확 경로)

```
web/package.json                                    # devDep "@tanstack/query-async-storage-persister", "@tanstack/react-query-persist-client", "idb-keyval"
web/src/App.tsx                                     # QueryClientProvider → PersistQueryClientProvider 교체 (Stage 2)
web/src/lib/persist-query.ts                       # 신규 — persister 생성 + buster + maxAge + dehydrate filter + tenant key (~120줄)
web/src/lib/persist-query.test.ts                  # 신규 — buster · dehydrate filter 단위 테스트 (~150줄)
web/src/lib/idb-storage.ts                         # 신규 — idb-keyval wrap + clear 헬퍼 (~40줄)
web/src/lib/idb-storage.test.ts                    # 신규 — fake-indexeddb 단위 테스트 (~80줄)
web/src/stores/auth.ts                             # clearSession에 persistor.removeClient + queryClient.clear 콜백 결선 (Stage 3)
web/src/api/hooks.ts                               # useLogout에 동일 결선 (Stage 3)
web/src/components/StaleDataBadge.tsx              # 신규 (선택, D-PWAPER-7) — hydrated 캐시에 "마지막 갱신 X분 전" badge (~50줄)
web/src/i18n/dict.ts                               # i18n 키 2종 (`pwa.persist.staleDataLabel`, `pwa.persist.hydrating`) ko + en
docs/operations/pwa-offline.md                     # §3 추가 — read 캐시 정책 + 민감 데이터 차단 list + clear 절차
```

### 6.2 `web/src/lib/persist-query.ts` 변경 outline

```ts
// 의사코드 — 코드 0줄 design doc, 실제 구현은 Stage 2.
import { createAsyncStoragePersister } from '@tanstack/query-async-storage-persister'
import { get, set, del } from 'idb-keyval'

const BUSTER = import.meta.env.VITE_BUILD_HASH ?? 'dev'  // 새 deploy 시 자동 무효화
const MAX_AGE = 7 * 24 * 60 * 60 * 1000                  // 7일 (D-PWAPER-3)

const DENY_PREFIXES: ReadonlyArray<ReadonlyArray<string>> = [
  ['sso'], ['webhooks'], ['invitations'], ['advisor'],
]  // D-PWAPER-5

export function makePersister(tenantId?: string) {
  const key = `rosshield-rq-${tenantId ?? 'anon'}`
  return createAsyncStoragePersister({
    storage: { getItem: (k) => get(k), setItem: (k, v) => set(k, v), removeItem: (k) => del(k) },
    key,
  })
}

export function shouldDehydrateQuery(query): boolean {
  // mutation은 자동 제외 (react-query 기본). queryKey prefix deny.
  return !DENY_PREFIXES.some(prefix => prefix.every((p, i) => query.queryKey[i] === p))
}

export const PERSIST_OPTIONS = { buster: BUSTER, maxAge: MAX_AGE, dehydrateOptions: { shouldDehydrateQuery } }
```

### 6.3 `web/src/App.tsx` 변경 outline

```tsx
// 의사코드.
import { PersistQueryClientProvider } from '@tanstack/react-query-persist-client'
import { makePersister, PERSIST_OPTIONS } from '@/lib/persist-query'
import { useAuthStore } from '@/stores/auth'

// QueryClientProvider → PersistQueryClientProvider 교체.
const tenantId = useAuthStore(s => s.user?.tenantId)
const persister = useMemo(() => makePersister(tenantId), [tenantId])

return (
  <PersistQueryClientProvider client={queryClient} persistOptions={{ persister, ...PERSIST_OPTIONS }}>
    <RouterProvider router={router} />
  </PersistQueryClientProvider>
)
```

### 6.4 logout 시 clear 결선 (`web/src/stores/auth.ts` + `useLogout`)

```ts
// auth.ts clearSession 호출 시점에 외부 콜백 호출 — store는 react-query/persistor 직접 참조 안 함(레이어 분리).
// 대신 useLogout(`hooks.ts:73`)이 logout 후 명시 호출:
//   await persister.removeClient()
//   queryClient.clear()
```

### 6.5 stale data badge UX (선택)

- `useQuery`의 `dataUpdatedAt`을 활용 — 현재 시간 - dataUpdatedAt > 5분이면 "n분 전" 표시.
- 옵션 D-PWAPER-7 결정에 종속 — default `enabled` 권장(사용자 신뢰성 ↑).

### 6.6 `docs/operations/pwa-offline.md` §3 추가 outline

- **§3.1 read 캐시 정책**: 7일 maxAge + buster(build hash) + tenant scope DB.
- **§3.2 민감 데이터 차단 list**: D-PWAPER-5 deny list 명시.
- **§3.3 clear 절차**: 사용자 logout 자동 / 운영자 수동(DevTools Application > IndexedDB > rosshield-rq 삭제).
- **§3.4 quota 초과 대응**: navigator.storage.estimate API로 용량 모니터 + 80% 도달 시 자동 prune.

---

## 7. TDD Stage 분해 (옵션 B 채택 시)

각 Stage 별 commit. 권장 분리 — **3 commit** (Stage 4는 Stage 3에 흡수).

### Stage 1 — `idb-storage` + `persist-query` 단위 모듈 + 단위 테스트 — 1 commit

- `web/package.json`에 `@tanstack/query-async-storage-persister` + `@tanstack/react-query-persist-client` + `idb-keyval` + `fake-indexeddb` (devDep 테스트용) 추가 + `pnpm install`.
- `web/src/lib/idb-storage.ts` 신규 — idb-keyval get/set/del wrap + tenant key 헬퍼.
- `web/src/lib/persist-query.ts` 신규 — `makePersister` + `shouldDehydrateQuery` + `PERSIST_OPTIONS` (buster + maxAge + dehydrate filter).
- 단위 테스트:
  - `idb-storage.test.ts` — fake-indexeddb로 get/set/del round-trip + tenant key 분리 검증.
  - `persist-query.test.ts` — `shouldDehydrateQuery` deny list 4 prefix 검증 + 화이트리스트 5 prefix(robots/scans/fleets/packs/me) 검증 + maxAge/buster 옵션 정상 전달.
- **검증**: `pnpm test` PASS, App.tsx 미연결(회귀 0).
- **이 commit만으로는 영속 0** — Stage 2에서 결선.

### Stage 2 — `App.tsx` PersistQueryClientProvider 결선 + 통합 테스트 — 1 commit

- `web/src/App.tsx` `QueryClientProvider` → `PersistQueryClientProvider` 교체.
- `useAuthStore`에서 tenantId 구독 → tenant 변경 시 새 persister(`useMemo` 의존성).
- `import.meta.env.VITE_BUILD_HASH` Vite define 추가 (`web/vite.config.ts`).
- 통합 테스트(RTL):
  - 첫 렌더 시 hydrate 시도 + 빈 캐시일 때 정상 진행.
  - hydrate된 캐시가 maxAge 초과 시 무시.
  - tenant 변경 시 새 persister key 생성.
- **검증**: vitest + tsc + Playwright e2e(기존) PASS, 사용자 로그인 후 새로고침 → 이전 query 캐시 표시 + 백그라운드 갱신 확인.

### Stage 3 — logout / clearSession 결선 + i18n + 운영자 docs + handoff — 1 commit

- `web/src/api/hooks.ts` `useLogout` mutationFn 끝에 `await persister.removeClient()` + `queryClient.clear()` 추가.
- `web/src/api/client.ts` 401 + refresh 실패 시 `clearSession` 직후 동일 호출(`auth.ts` 또는 별 헬퍼).
- (선택, D-PWAPER-7 default가 enable면) `web/src/components/StaleDataBadge.tsx` + 핵심 페이지(dashboard, robots, scans) 결선.
- `web/src/i18n/dict.ts` ko + en 2 키.
- `docs/operations/pwa-offline.md` §3 추가(§6.6 outline).
- `SESSION_HANDOFF.md` 진척 한 줄 + 결정 로그 D-PWAPER-1~7 채택 결과 한 줄.
- **검증**: vitest + tsc + Go test 50+ 패키지 PASS, 회귀 0. 수동 검증: logout 후 IndexedDB 비어있음 + 새 로그인 시 빈 캐시.

**총 ~1.5~2.5일** (Stage 1: 0.5일 — 모듈 + fake-indexeddb 테스트, Stage 2: 0.5~1.0일 — provider 결선 + RTL 통합, Stage 3: 0.3~0.5일 — 결선 + docs).

### Stage 4 (선택, 후속 epic) — multi-tab sync + quota 모니터

본 design doc 본 epic 비대상. 옵션 C 진입 후 customer feedback 기반 진입:
- BroadcastChannel로 multi-tab logout 동기화.
- `navigator.storage.estimate()` 폴링 → 80% 도달 시 자동 prune(가장 오래된 query 삭제) + UX 안내.
- 더 fine-grained tenant 분리(현재는 tenant ID 단순 접두 + DB scope 분리 미구현).

---

## 8. 결정 항목 (D-PWAPER-N)

각 항목 권장 default 명시 — trigger 발화 시 즉시 진입 부담 0.

### D-PWAPER-1 — persistor 선택 (sync vs async + storage)

**선택지**:
1. `@tanstack/query-sync-storage-persister` + localStorage (옵션 A)
2. **`@tanstack/query-async-storage-persister` + IndexedDB(idb-keyval)** ← **권장 default**
3. `@tanstack/react-query-persist-client` + 자체 IndexedDB wrapper (옵션 C)
4. 보류 (옵션 D)

**근거**: IndexedDB 50MB+는 scan 결과 누적 시 충분, localStorage 5MB는 어플라이언스 PoC 30일에서 부족 가능. async는 메인 스레드 비차단으로 첫 페인트 즉시. idb-keyval(~3KB)는 광범위 사용 검증된 라이브러리 — 자체 wrapper는 회귀 위험 ↑. trigger 발화 전이라면 옵션 D(보류) 우선.

### D-PWAPER-2 — multi-tenant storage key 정책

**선택지**:
1. **단일 key + tenant 변경 시 invalidate** — `rosshield-rq` (단순)
2. **tenant 별 key 접두** — `rosshield-rq-${tenantId}` ← **권장 default**
3. tenant 별 별 IndexedDB DB — `rosshield-rq` DB + `rq-${tenantId}` object store

**근거**: tenant 별 key 접두는 단일 origin SSO 다중 tenant 시나리오에서 사용자 A 캐시 ↔ 사용자 B 로그인 누설 사전 차단. 단일 key 방식은 logout 시 clear에 의존 — race 위험. 별 DB 방식은 quota 관리 복잡 + 가치 부족.

### D-PWAPER-3 — maxAge (캐시 만료)

**선택지**:
1. 1일 (24h)
2. **7일 (168h)** ← **권장 default**
3. 30일
4. 무제한 (buster만)

**근거**: 7일은 어플라이언스 점검 cycle(주 1회)와 정합. 1일은 짧아 가치 ↓, 30일은 stale 위험 ↑. 무제한은 build hash buster만으로 갱신 → 빌드 자주 시 OK이나 패키지 갱신 주기 불확실 시점에서 보수적 중간값 7일 권장.

### D-PWAPER-4 — multi-tenant cache clear 시점

**선택지**:
1. **logout (`useLogout`) + 401 refresh 실패 (`client.ts`)** ← **권장 default**
2. logout만(401 refresh 실패는 자동 회복 가능 → clear 불필요)
3. logout + tenant 전환(SSO) + 401

**근거**: logout은 사용자 명시 의도 → 캐시 clear 필수. 401 refresh 실패는 사용자 세션 종료(`clearSession` 호출) → 동일 처리 정합. tenant 전환(SSO 다중 tenant)는 D-PWAPER-2 tenant 별 key로 자동 분리 → 명시 clear 불필요(naturally orphaned).

### D-PWAPER-5 — 민감 데이터 차단 list (`shouldDehydrateQuery` deny prefix)

**선택지**:
1. **opt-out (allow by default + deny list `[['sso'], ['webhooks'], ['invitations'], ['advisor']]`)** ← **권장 default**
2. opt-in (deny by default + allow list `[['robots'], ['scans'], ['fleets'], ['packs'], ['me']]`)
3. 차단 0 (전 query 영속)
4. 차단 0 + 응답 본문 sanitize(서버 측 redact 강제)

**근거**: opt-out은 본 epic 핵심 가치(read 캐시) 즉시 충족 + 보안 위험 부분만 차단. opt-in은 새 hook 추가 시 allow list 누락 = cache 가치 0(보수적 — `feedback_design_doc_conservative.md`와 별개로 가치 측면). 차단 0은 SSO clientSecret + invitation token 노출 위험 ↑. 서버 측 sanitize는 별 epic(기존 redact 동작 신뢰).

### D-PWAPER-6 — buster (앱 버전 기반 캐시 무효화)

**선택지**:
1. **build hash (`import.meta.env.VITE_BUILD_HASH`)** ← **권장 default**
2. SemVer 버전 (`web/package.json` version)
3. 수동 환경 변수 (`VITE_PERSIST_BUSTER`)
4. buster 없음 (maxAge만)

**근거**: build hash는 모든 deploy에서 자동 변경 → SW Stage 2 갱신 후 stale 자산 방지와 정합. SemVer는 패키지 버전 갱신 주기 불일치(매 deploy ≠ semver bump) → cache 일관성 ↓. 수동은 운영자 부담. buster 없음은 maxAge 7일이 사실상 buster 역할(중복).

### D-PWAPER-7 — stale data UX badge

**선택지**:
1. **enable (`StaleDataBadge` 핵심 페이지에 결선, "n분 전" 표시)** ← **권장 default**
2. disable (badge 0, OfflineIndicator만)
3. enable + 5분 초과 시만 표시(noise 회피)

**근거**: enable은 사용자가 캐시 데이터를 최신으로 오인하는 위험 차단(원칙 §11 설명 가능성). disable은 UX 단순하나 audit 신뢰 손상 위험. 5분 임계는 실제 사용자 feedback 기반 후속 조정.

---

## 9. 회귀 위험 / 운영 고려

### 9.1 기존 zustand persist 영향

- zustand persist(`rosshield-auth`)는 localStorage, react-query persist는 IndexedDB → **storage 완전 분리 + 충돌 0**.
- accessToken은 zustand가 단일 진실 근원 — react-query 캐시는 hydrate 후 request header로 부착.
- logout 시 두 storage 모두 clear 필수: zustand `clearSession()` + persister `removeClient()` + `queryClient.clear()` (D-PWAPER-4).

### 9.2 보안 — 민감 토큰 IndexedDB 노출 방어

- **accessToken은 IndexedDB에 절대 안 저장** — zustand 영역, hooks.ts hook 본문은 token을 응답에 포함 안 함.
- **민감 응답 차단 list**(D-PWAPER-5) — SSO clientSecret + webhook secret + invitation token + advisor 대화는 dehydrate 단계에서 제외.
- **XSS 위험**: react-query persist는 origin scope — 같은 origin의 XSS는 어차피 localStorage(accessToken)도 노출 가능. 본 epic은 추가 위험 0(이미 노출된 동일 origin scope).
- **디바이스 탈취**: 사용자 디바이스 도난 시 IndexedDB 평문 read 가능. **mitigation**: logout 시 자동 clear + 24h refresh token 만료 시 자동 무효화(서버 측). 추가 암호화는 별 epic(crypto.subtle + webcrypto + key 관리 복잡 → ROI 낮음).
- 회귀 테스트: `persist-query.test.ts`에 deny list 4 prefix 모두 영속 차단 + allow list 5 prefix 영속 OK 검증 필수.

### 9.3 Storage quota

- IndexedDB 기본 quota: Chrome ~60% 디스크 / Firefox ~50% / Safari ~1GB. 어플라이언스 NUC(SSD 256GB+)에서 충분.
- **위험 시나리오**: scan 결과 100대 × 30회 × 100 finding = 300k row × ~500 bytes = ~150MB. quota 초과 가능.
- **mitigation**: maxAge 7일(D-PWAPER-3)로 자동 prune + Stage 4(후속 epic) `navigator.storage.estimate()` 모니터.
- 회귀 테스트: 큰 캐시 시 quota exceeded 에러 silent fail 안 함 검증 — react-query 표준 동작 확인.

### 9.4 SW 갱신 시 cache 무효화

- vite-plugin-pwa Stage 2 SW 갱신 시 `__APP_VERSION__` 또는 build hash가 새 값 → buster 변경 → react-query cache 자동 무효화.
- SW와 react-query는 직교 — SW는 정적 자산, react-query는 API 응답.
- 회귀: SW 갱신 후 사용자가 새 빌드 진입 시 캐시 1회 손실(예상 동작) → UX 한 줄 안내(StaleDataBadge가 "마지막 갱신 0분 전"으로 즉시 표시).

### 9.5 첫 paying customer 진입 시점 영향

- **데모 신뢰성 ↑↑** — 데모 중 일시 단절 + 새로고침에도 데이터 유지 → "production-grade" 인상.
- **어플라이언스 PoC 30일 운영** — 사용자 노트북 하루 1~2회 일시 단절 시나리오에서 incident 0.
- **multi-tenant SSO** — D-PWAPER-2 tenant 별 key로 자동 격리 → SSO 다중 tenant 데모 시 누설 위험 0.

### 9.6 customer 진입 전 보류의 가치

- 옵션 D(보류) 채택 시 trigger 발화까지 코드 0 → 사후 후회 0.
- 본 design doc만으로 trigger 발화 시 1.5~2.5일 즉시 진입 가능 → lead time 짧음.
- 보안 정책(D-PWAPER-5 deny list)은 customer 환경 검토 후 확정 가능.

### 9.7 한계 (의도적)

- **mutation 오프라인 queueing 미실행** — `pwa-offline-design.md` §3.5 비요구 정렬. audit chain leader epoch 정합성 위험.
- **암호화 미실행** — IndexedDB 평문. crypto.subtle 적용은 별 epic(key 관리 + 성능 영향 분석 필요).
- **multi-tab sync 미실행** — 동일 사용자 다중 탭에서 logout 시 다른 탭 stale 가능. BroadcastChannel은 Stage 4 후속.
- **Tauri 데스크톱 직교** — Tauri는 별 storage(SQLite plugin) 사용 가능 → 본 epic은 web 브라우저 환경만.

---

## 10. 참조

### 관련 design doc

- 직전 epic: `docs/design/notes/pwa-offline-design.md` §3.4(시나리오) + §4.3(옵션 C 정의) + §7 Stage 6(trigger).
- `docs/design/01-principles.md` §3 (에어갭 1급) — 본 epic의 정책 근거.
- `docs/design/01-principles.md` §10 (프라이버시 기본값 — 로컬 우선) — IndexedDB 영속 정합.
- `docs/design/01-principles.md` §11 (설명 가능성) — stale data badge UX 정합.
- `docs/design/01-principles.md` §12 (점진적 적용) — 옵션 D 보류 → 옵션 B 진화 권장.
- `docs/design/04-domain-and-data-model.md` — multi-tenancy tenant_id scope 일관 (D-PWAPER-2).
- `docs/design/06-security-and-tenancy.md` — 민감 데이터 차단 list(D-PWAPER-5) 근거.
- `docs/design/09-ui-and-clients.md` §9.2 — Web Console 스택 정의.
- `docs/design/phase5-backlog.md` — 본 epic 등재 위치 (PWA persist 별 라인 — 등재 후속).

### 코드 파일 (현재 상태)

- `web/package.json` (devDep 후보 위치 — Stage 1)
- `web/src/App.tsx:24-34` (`QueryClient` 생성 — `PersistQueryClientProvider` 결선 위치, Stage 2)
- `web/src/api/hooks.ts:73-89` (`useLogout` — clear 결선 위치, Stage 3)
- `web/src/api/hooks.ts` (전 30+ useQuery 위치 — D-PWAPER-5 deny list 검증 대상)
- `web/src/api/client.ts:71-82` (401 refresh 실패 → `clearSession` — clear 결선 위치, Stage 3)
- `web/src/stores/auth.ts:52` (zustand `clearSession` — react-query persist clear 콜백 트리거)
- `web/src/lib/use-is-offline.ts` (Stage 3 결선 — react-query persist는 offline 무관, 직교)
- `web/src/components/OfflineIndicator.tsx` (Stage 3 — 본 epic은 별 영역)
- `docs/operations/pwa-offline.md` (Stage 3 — §3 추가 대상)

### 외부 참조 (최소)

- `@tanstack/react-query-persist-client`: <https://tanstack.com/query/latest/docs/framework/react/plugins/persistQueryClient>
- `@tanstack/query-async-storage-persister`: <https://tanstack.com/query/latest/docs/framework/react/plugins/createAsyncStoragePersister>
- `@tanstack/query-sync-storage-persister`: <https://tanstack.com/query/latest/docs/framework/react/plugins/createSyncStoragePersister>
- `idb-keyval`: <https://github.com/jakearchibald/idb-keyval> (~3KB IndexedDB key-value wrapper).
- `fake-indexeddb` (devDep, Stage 1 단위 테스트): <https://github.com/dumbmatter/fakeIndexedDB>.

### 메모리 패턴

- `feedback_design_doc_first.md` — 1.5~2.5일 작업이 본 design doc 우선 정책 적용 대상.
- `feedback_design_doc_conservative.md` — 추정 시간/효과 보수적 (옵션 B 1.5~2.5일은 단위 테스트 + 운영자 docs + handoff 포함 보수치).
- `feedback_parallel_agents.md` — Stage 1(idb-storage 모듈) + Stage 3(docs)는 영역 분리로 sub-agent 병렬 가능. Stage 2(App.tsx 결선)는 단일 영역.
- `feedback_no_rest_recommendation.md` — 본 design doc은 코드 0 산출물 → 작업 후 즉시 commit.

### 결정 로그 후속 (epic 진입 시)

- D-PWAPER-1~7 채택 결과는 `SESSION_HANDOFF.md` 결정 로그에 한 줄 기록 (날짜 + 옵션 채택 + Stage 1 commit hash).
- trigger 발화 사실 + 발화 사유(customer 명시 or PoC 보고)도 결정 로그에 명시.
