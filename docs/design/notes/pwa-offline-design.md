# PWA / 오프라인 지원 — 에어갭 1급 강화 design doc

> **상태**: Phase 5 carryover (`SESSION_HANDOFF.md`의 별 epic 후보로 명시). 본 문서는 **코드 0줄 / 마이그레이션 0** — 합성 옵션 비교 + Stage 분해 + D-PWA-1~8 결정 항목 권장 default 까지만 마감합니다.
> **참조**: 직전 web epic E10 (Stage A·B·C·D, `internal/web/embed.go`) + Phase 5 RBAC Stage 2-E 직후 큰 작업 design doc 우선 정책(memory `feedback_design_doc_first.md`).
> **R 식별자**: 별도 R 미할당 (Phase 5 backlog "PWA / 오프라인 지원 — vite-plugin-pwa" 항목으로 등재 예정).

---

## 1. 배경

### 1.1 왜 별 epic인가

rosshield의 핵심 설계 원칙 §3 "에어갭 1급 (Air-gap First-class)"는 외부 네트워크 단절 환경에서 **완전 동작**을 요구합니다. 현재 `rosshield-server` 단일 바이너리는 정적 자산 + API + audit chain을 자체 포함(P3·P7)하나, **Web Console 자체는 백엔드 단절 시 진입조차 못 합니다**. 이는 다음 운영 시나리오에서 사용자 경험 단절을 야기합니다.

- **on-prem 어플라이언스 점검 중 사용자 노트북 연결 단절** — NUC/OptiPlex 어플라이언스(Phase 4 backlog R30·R40, E33 snap 이미 마감)에 직접 연결한 감사인이 IP 변경·VPN 재인증·LAN 케이블 빠짐 등으로 일시 끊기면 마지막 dashboard 화면까지 사라집니다.
- **태블릿/스마트폰 현장 회수** — 감사인이 모바일에서 install 가능한 web app으로 대시보드 빠르게 확인하는 시나리오 부재.
- **첫 paying customer 데모 중 네트워크 hiccup** — Demo 환경 일시 단절 시 흰 화면 진입 → 신뢰 손상 위험.

**현 한계 1줄**: 백엔드 응답 단절 시 모든 페이지 흰 화면(react-query 캐시는 메모리 only, persist 없음).

### 1.2 PWA가 가져오는 가치 (3종)

1. **App shell 캐싱** — service worker가 HTML/JS/CSS/fonts를 캐시 → 백엔드 단절에도 UI 진입 + 메뉴 + 마지막 데이터 read OK.
2. **Install 지원** — manifest.json + display:standalone + 아이콘 → 데스크톱·모바일 홈 화면 추가 가능 (Tauri 데스크톱은 별 트랙).
3. **읽기 전용 fallback UX** — 오프라인 indicator + mutation 차단 + 명확한 안내 = 원칙 §11 "설명 가능성" 정합.

### 1.3 잠재 효과 / 추정 (보수적)

- **잠재 효과**: 직접적 매출 0. 운영 신뢰성·UX·에어갭 정합성. 첫 paying customer 데모 안정성 ↑.
- **추정 시간**: 옵션 A 1.5~2.0일 (vite-plugin-pwa generateSW + 기본 manifest), 옵션 C 2.5~3.5일 (+ react-query persist), 옵션 B 3.0~5.0일 (수동 SW), 옵션 D 0.5일 (manifest만).
- **회귀 위험**: 중. SW는 캐시 정책 잘못 시 stale 자산이 사용자 환경에 영구 잔존 가능 — 갱신 정책·skipWaiting·rollback 절차 필수.

---

## 2. 현재 상태 진단

### 2.1 web/ 디렉토리 구성

| 항목 | 값 | 출처 |
|---|---|---|
| 빌드 도구 | Vite 6 + React 19 + TypeScript 5.7 | `web/package.json` |
| 라우팅 | TanStack Router v1 file-based + autoCodeSplitting | `web/vite.config.ts` |
| 데이터 fetch | TanStack Query v5 (`@tanstack/react-query`) + openapi-fetch | `web/src/api/{client,hooks}.ts` |
| 상태 (인증) | Zustand persist + localStorage `rosshield-auth` (accessToken + user) | `web/src/stores/auth.ts` |
| 상태 (테마) | Zustand persist (별 store) | `web/src/stores/theme.ts` |
| 빌드 outDir | `../internal/web/dist/` (vite.config.ts) | `web/vite.config.ts:41` |
| Go embed | `//go:embed dist` (`internal/web/embed.go`) | `internal/web/embed.go:30-31` |
| 정적 서빙 | `Handler()` — `/assets/*` immutable cache 1년 + SPA fallback (unmatched → index.html) + GET/HEAD only | `internal/web/embed.go:50-96` |
| WebSocket | `useScanProgress` — `/api/v1/scans/{sessionId}/progress` (`new WebSocket()`, polling fallback) | `web/src/api/hooks.ts:1241-1322` |

### 2.2 현 정적 자산 / index.html 상태

- `web/index.html`은 13줄 minimal — `<head>`에 `<link rel="icon" type="image/svg+xml" href="/favicon.svg" />`만 있고 **manifest.json/theme-color/apple-touch-icon 모두 부재**.
- `web/public/` 디렉토리는 **빈 상태** (favicon.svg 자체도 없음 — 404 inert).
- service worker 미등록 (`navigator.serviceWorker.register()` 호출 0 — 전 코드베이스 grep).

### 2.3 react-query 캐시 정책

- `web/src/App.tsx`의 QueryClient 기본값:
  - `staleTime: 30_000` (30초)
  - `refetchOnWindowFocus: false`
- **persist 어댑터 없음** — 페이지 새로고침 시 react-query 캐시 전부 휘발 (메모리 only).
- 따라서 백엔드 단절 + 새로고침이 동시에 일어나면 모든 데이터 손실.

### 2.4 인증 토큰 storage

- `accessToken` (단명) — Zustand persist → `localStorage` key `rosshield-auth` (평문).
- `refreshToken` — 서버가 HttpOnly cookie로 관리 (C5+C6 결정), 브라우저 JS 접근 불가.
- **SW가 fetch intercept 시 cookie는 자동 동봉** (브라우저 표준), Authorization 헤더는 react-query/openapi-fetch 미들웨어가 부착 — SW는 단순 통과시키면 됨.

### 2.5 WebSocket 사용

- `useScanProgress` 단 한 곳. WebSocket은 SW의 fetch handler 대상이 아님 (SW는 fetch event만 intercept). **PWA SW는 WebSocket과 직교** — 별도 처리 불필요.
- 단, 오프라인 시 WS 자체가 실패하므로 polling fallback도 실패 → status='error' 표시 동작 그대로.

### 2.6 Go embed 영향 점검

- `//go:embed dist` 패턴은 `dist/` 트리 전체를 흡수 — manifest.json·sw.js·icons도 자동 포함.
- `Handler()`는 `/assets/*`만 immutable cache, 나머지는 stat 기반 → SW 파일은 무관(아래 §3.4 참조).
- **변경 필요 없음 가설**: SW 파일 cache header는 SW 자체가 결정 (브라우저는 SW 본체에 대해 24h max cache 강제) — Go handler 추가 헤더 정책 무관.

---

## 3. 요구 사항 분류

### 3.1 App shell 캐싱 (필수)

- HTML(index.html) + JS chunks + CSS + fonts + 아이콘 = service worker pre-cache.
- 캐시 전략: `precache` + `staleWhileRevalidate` (자산 hash 기반 immutable 보장 — Vite가 파일명에 hash 부착).
- 첫 로드 후 백엔드 단절 → 메뉴 + 페이지 셸 + 라우터 동작 OK.

### 3.2 API 캐시 정책 (선택, 옵션 C에서)

- **GET endpoint**: react-query 캐시를 IndexedDB에 persist (사용자 새로고침/오프라인 후에도 마지막 결과 표시).
- **Mutation (POST/PUT/DELETE)**: SW가 `navigator.onLine === false`이면 즉시 실패 응답 + 사용자 안내 (queueing 미구현 — 데이터 정합성·audit chain leader epoch 위험).
- WebSocket: SW 직교 (위 §2.5).

### 3.3 Install 지원 (필수)

- `manifest.json`: name, short_name, description, theme_color, background_color, display: standalone, icons (192·512 PNG).
- `<link rel="manifest" href="/manifest.json">` index.html에 추가.
- `<meta name="theme-color">` 추가.
- iOS는 manifest 표준 미흡 → `<link rel="apple-touch-icon">` 별도 (옵션 D-PWA-3).

### 3.4 에어갭 시나리오 정의

| 시나리오 | 기대 동작 | 옵션 A 충족 | 옵션 C 충족 |
|---|---|---|---|
| 첫 로드 + 백엔드 단절 | 셸은 보이나 데이터 0 (로그인 페이지만 가능) | ✅ | ✅ |
| 인증 후 사용 중 백엔드 단절 | 메뉴 + 마지막 페이지 데이터 read OK | △ (메뉴만) | ✅ (read-only) |
| 백엔드 단절 + 새로고침 | 셸 + 로그인된 상태(localStorage) | ✅ | ✅ |
| 오프라인 + mutation 시도 | 명확 에러 + "오프라인" indicator | ✅ | ✅ |
| 오프라인 + WebSocket | scan progress 불가 (정상 — 별 처리 없음) | ✅ | ✅ |

### 3.5 비요구 (의도적 제외)

- **Background sync** — 오프라인 mutation queueing 후 자동 재시도. 데이터 정합성 위험(audit chain leader epoch + RBAC 등) → 명시 비목표.
- **Push notification** — 외부 push 서비스 의존(Firebase/web-push), 에어갭 §3 위반.
- **오프라인 audit chain 검증** — `report verify` CLI(E30) 또는 별 데스크톱(Tauri) 경로 권장. PWA 본 epic에서 미실행.

---

## 4. 합성 전략 옵션 (≥3)

### 4.1 옵션 A — vite-plugin-pwa + Workbox `generateSW` 모드

**범위**: vite-plugin-pwa 0.20+ (Workbox 7) devDep 추가 + `vite.config.ts` plugin 등록 + manifest.json 인라인 정의 + 아이콘 2종(192·512). 기본 generateSW는 build 시 SW 코드를 자동 생성 — Vite 빌드 산출물(JS/CSS/HTML) 자동 precache + 런타임 캐시 룰 inline.

**Pros**:
- 가장 단순. ~50줄 vite.config.ts 변경 + 아이콘 + 매뉴얼 SW 코드 0.
- Workbox가 캐시 갱신·skipWaiting·navigation fallback 모범 사례 자동 적용.
- Vite hash 기반 자산명 → revisioning 자동.
- vite-plugin-pwa 광범위 사용(2k+ stars), Vite 6 + React 19 호환.

**Cons**:
- API 캐시 정책 제어 한계 (Workbox 룰만 — 복잡 invalidation은 어려움).
- Google Workbox 외부 dep ON (사이즈 ~30KB minified).
- runtime caching 룰을 generateSW 인자로 작성 — 복잡한 분기 어려움.

**회귀 위험**: 중. SW는 한번 install되면 사용자 브라우저에 영구 잔존 → 오류 SW 배포 시 rollback 비용 ↑. dev 모드 기본 비활성(`devOptions.enabled=false`)으로 디버깅 단순화.

**코드 변경 추정**:
- `web/package.json` devDep `vite-plugin-pwa@~0.20` 1줄.
- `web/vite.config.ts` plugin 등록 + manifest 인라인 + workbox 룰 ~40줄.
- `web/public/icon-192.png` + `web/public/icon-512.png` + `web/public/favicon.svg` (현재 부재 — 별 fix).
- `web/index.html` `<link rel="manifest">` + `<meta name="theme-color">` 추가.
- 단위 테스트: Vitest로 manifest.json 존재·유효 JSON 검증 (~30줄).
- 통합 테스트: Go `internal/web/embed_test.go`에 manifest.json + sw.js 임베드 회귀 테스트 (~20줄).
- 총 ~150줄 신규 (코드+테스트), 외부 dep +1.

**운영 영향**: 사용자 브라우저에 SW 1회 install 후 background 갱신. 첫 출시 시 `clients.claim()` + skipWaiting 정책으로 즉시 활성, 이후 갱신은 24h 또는 강제 reload.

### 4.2 옵션 B — 수동 service worker + manifest.json (Workbox 미사용)

**범위**: 직접 `web/src/sw.ts` 작성 + Vite plugin 없이 빌드 후 별 처리 + 매뉴얼 등록.

**Pros**:
- 외부 dep 0.
- SW 동작 100% 통제 (캐시 룰·갱신 정책 직접 제어).
- 코드 의도 투명 (감사·보안 리뷰 단순).

**Cons**:
- 작성·테스트 비용 ↑ (~200~400줄).
- Vite hash 자산 manifest 직접 추출 필요 (rollup output API 또는 별 빌드 hook).
- Workbox 모범 사례(navigation route + precache + activate) 직접 재구현.
- 회귀 디버깅 부담 — 캐시 룰 1줄 실수가 사용자 브라우저 영구 잔존.

**회귀 위험**: 큼. 자체 코드 = 모든 edge case 자체 책임. precache manifest 누락 시 부분 자산만 캐시 → 회로 차단 발생.

**코드 변경 추정**: ~300~500줄 (sw.ts + 빌드 hook + 등록 코드 + 테스트).

**운영 영향**: 옵션 A와 동일 (한번 install 후 갱신).

### 4.3 옵션 C — 옵션 A + react-query persist (IndexedDB)

**범위**: 옵션 A 전체 + `@tanstack/query-sync-storage-persister` + `@tanstack/react-query-persist-client` 추가 + IndexedDB 어댑터 + persist key 설정.

**Pros**:
- §3.2 read-only 캐시까지 충족 — 오프라인 + 새로고침 후에도 마지막 GET 결과 그대로.
- §3.4 모든 시나리오 ✅.
- 옵션 A의 모든 장점 + 데이터 read 가용성.

**Cons**:
- 의존성 +2 (~25KB).
- persist 키 정책 필요 (테넌트 분리·로그아웃 시 clear — D-PWA-7).
- stale data 표시 정책 정의 필요 (사용자가 오래된 데이터 인지 못 하면 신뢰 ↓ — 옵션 indicator + timestamp 명시).
- accessToken Bearer 헤더 만료 → 401 → react-query stale 그대로 → 사용자 혼란 가능.

**회귀 위험**: 중상. persist는 실제 실 사용 시 stale·다중 탭·로그아웃 race condition 등 edge case 多.

**코드 변경 추정**: 옵션 A + ~200줄 (persist provider + IndexedDB 어댑터 + clear 로직 + 테스트).

**운영 영향**: 사용자 브라우저 IndexedDB 추가 점유 (수십 MB 가능 — quota 관리 필요).

### 4.4 옵션 D — manifest.json + install prompt만 (SW 없이)

**범위**: manifest.json + 아이콘 + install prompt UI 가드만. SW 없이.

**Pros**:
- 가장 간단(0.5일).
- 외부 dep 0.
- install 가능 (홈 화면 추가) + theme-color 적용.

**Cons**:
- **에어갭 §3 가치 0** — 백엔드 단절 시 흰 화면 그대로(셸 캐시 없음). 본 epic의 핵심 가치 미충족.
- 단순히 "PWA installable"의 marketing 가치만.

**회귀 위험**: 0.

**코드 변경 추정**: ~50줄 + 아이콘 2종.

**운영 영향**: 0.

---

## 5. 권장 옵션 + 근거

**권장 default: 옵션 A — vite-plugin-pwa generateSW 모드**.

**근거**:

1. **에어갭 §3 가치 핵심 충족** — 백엔드 단절에도 셸 + 메뉴 + 마지막 페이지 진입 OK. 옵션 D는 이 가치 0이므로 epic 자체 의미 상실. 옵션 C는 이 가치 + read-only 데이터까지 충족하나 본 epic 첫 진입에는 과스코프(아래 항목).

2. **운영 복잡도 vs 이득 대비 균형** — 옵션 A는 ~150줄 + Workbox dep 1개로 epic 핵심 가치 100% 달성. 옵션 B(수동 SW)는 SW 코드 자체가 회귀 위험 폭탄, 작성·디버깅 비용이 epic 가치를 초과. 옵션 C는 read-only 캐시가 시나리오 §3.4의 일부에서만 결정적 가치 — 첫 paying customer 데모 단계까지는 옵션 A로 시연 + 신뢰 확보 후 customer feedback 기반으로 옵션 C 추가가 ROI 우위.

3. **회귀 위험 격리** — vite-plugin-pwa는 광범위 사용 + 검증된 기본값 (Workbox 7 navigation fallback + precache + skipWaiting + clientsClaim). 자체 SW 작성보다 회귀 위험 1/5 수준.

4. **보안 표면 최소** — Workbox는 same-origin SW + 외부 push/sync 비활성 기본값. 외부 fetch 0, telemetry 0. P3 정합.

5. **점진 적용 (P12)** — 옵션 A → 옵션 C 확장은 SW 갱신만으로 가능 (사용자 영향 1회 갱신). 옵션 A에서 시작, customer feedback 기반 read-only 필요성 명확해지면 옵션 C로 진화.

6. **회귀 차단 비용 작음** — 첫 출시 시 `clients.claim()`로 즉시 활성 + Workbox 자동 revisioning + 비상 시 빈 SW(unregister-only) 배포로 사용자 영향 30분 내 복구 가능.

**대안 default**: 사용자가 "오프라인 read까지 핵심 가치"라고 명시 시 → **옵션 C**. 본 design doc Stage 분해는 옵션 A 기준 + Stage 6 옵션 C 진입 trigger 명시.

**기각 근거 요약**:
- 옵션 B: ROI 부재. Workbox는 SW 모범 사례 표준. 자체 작성 = 모든 edge case 자체 책임 + 외부 dep 회피의 가치는 epic 단독으로 정당화 안 됨.
- 옵션 D: 에어갭 §3 가치 0. epic 자체 의미 상실.

---

## 6. 변경 사항 outline (옵션 A 채택 시)

### 6.1 신규 / 수정 파일 (정확 경로)

```
web/package.json                               # devDep "vite-plugin-pwa@~0.20" 추가
web/vite.config.ts                             # VitePWA plugin import + plugins 배열에 등록
web/index.html                                 # <link rel="manifest"> + <meta name="theme-color"> 추가
web/public/manifest.webmanifest                # vite-plugin-pwa가 자동 생성 (config 인라인 정의로)
web/public/favicon.svg                         # 신규 (현재 index.html 참조하나 부재 상태)
web/public/icon-192.png                        # 신규 (D-PWA-3 결정에 따라 SVG → PNG 자동 변환 또는 수동)
web/public/icon-512.png                        # 신규
web/public/apple-touch-icon.png                # iOS 호환 (180x180)
web/src/sw.ts                                  # injectManifest 모드 채택 시만 (D-PWA-1)
web/src/lib/pwa-update.ts                      # SW 갱신 알림 hook (~80줄)
web/src/components/UpdatePrompt.tsx            # 신규 SW 발견 시 toast (선택)
web/src/components/OfflineIndicator.tsx        # navigator.onLine 기반 banner (~30줄)
web/src/lib/pwa-update.test.ts                 # 단위 테스트 (~50줄)
internal/web/embed_test.go                     # manifest + sw 임베드 회귀 테스트 추가 (~20줄)
docs/operations/pwa-deployment.md              # 운영자 가이드 신규 (~150줄)
```

### 6.2 vite.config.ts 변경 outline

```ts
import { VitePWA } from 'vite-plugin-pwa'

plugins: [
  TanStackRouterVite({...}),
  react(),
  tailwindcss(),
  VitePWA({
    registerType: 'prompt',          // skipWaiting 사용자 동의 (D-PWA-6)
    devOptions: { enabled: false },  // dev 캐시 디버깅 부담 회피
    manifest: {
      name: 'rosshield Console',
      short_name: 'rosshield',
      description: 'ROS2 fleet security audit console',
      theme_color: '#0a0a0a',
      background_color: '#0a0a0a',
      display: 'standalone',
      icons: [
        { src: '/icon-192.png', sizes: '192x192', type: 'image/png' },
        { src: '/icon-512.png', sizes: '512x512', type: 'image/png' },
        { src: '/icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
      ],
    },
    workbox: {
      navigateFallback: '/index.html',
      navigateFallbackDenylist: [/^\/api\//],   // API는 SW navigation 우회
      globPatterns: ['**/*.{js,css,html,svg,png,woff2}'],
      runtimeCaching: [],                        // GET API는 react-query persist 별 트랙(옵션 C)
      cleanupOutdatedCaches: true,
    },
  }),
]
```

### 6.3 index.html 변경

- `<link rel="manifest" href="/manifest.webmanifest">`
- `<meta name="theme-color" content="#0a0a0a">`
- `<link rel="apple-touch-icon" href="/apple-touch-icon.png">`

### 6.4 SW 갱신 알림 (`web/src/lib/pwa-update.ts`)

vite-plugin-pwa의 `useRegisterSW` (`virtual:pwa-register/react`) 어댑터:
- `needRefresh` 상태 → toast 표시 → 사용자 클릭 시 `updateSW(true)` 호출하여 reload.
- `offlineReady` 상태 → 한 줄 안내 ("오프라인 사용 가능").

### 6.5 OfflineIndicator 컴포넌트

- `navigator.onLine` + `online`/`offline` 이벤트 리스너 → 상단 banner 표시.
- mutation 호출 직전 `useIsOffline` hook 가드 → toast로 차단 안내 + button disabled (선택, D-PWA-4).

### 6.6 embed_test.go 회귀 테스트

```go
func TestEmbedIncludesManifest(t *testing.T) {
  // dist/manifest.webmanifest 존재 + JSON 유효 (vite-plugin-pwa 자동 생성)
  // dist/sw.js 존재 + 비어있지 않음
}
```

---

## 7. TDD Stage 분해 (옵션 A 채택 시)

각 Stage 별 commit. 권장 분리 — **4 commit**.

### Stage 1 — manifest.json + 아이콘 + index.html (PWA installable, SW 없이) — 1 commit

- `web/public/icon-192.png` + `icon-512.png` + `apple-touch-icon.png` + `favicon.svg` 추가 (기존 index.html 참조 fix 동반).
- `web/index.html`에 `<link rel="manifest">` + `<meta name="theme-color">` 추가.
- `web/public/manifest.webmanifest` 정적 파일로 작성 (Stage 2에서 plugin이 덮어쓸 예정).
- 단위 테스트: manifest.webmanifest 유효 JSON + 필수 키 확인 (Vitest).
- `internal/web/embed_test.go` manifest 임베드 회귀.
- **검증**: `pnpm build` PASS + dist에 manifest 포함 + Lighthouse "Installable" PASS.
- **이 commit만으로도 옵션 D 가치 달성** — install prompt + 홈 화면 추가 가능.

### Stage 2 — vite-plugin-pwa generateSW 모드 도입 + SW 등록 — 1 commit

- `web/package.json`에 `vite-plugin-pwa@~0.20` devDep 추가 + `pnpm install`.
- `web/vite.config.ts` VitePWA plugin 등록 (위 §6.2 outline).
- manifest 정적 파일 제거 → plugin 인라인으로 위임.
- `web/src/main.tsx` 또는 `App.tsx`에서 SW 등록 import (`virtual:pwa-register/react`).
- 단위 테스트: 빌드 후 dist에 sw.js + workbox-* 자산 생성 검증.
- **검증**: build PASS + 첫 로드 후 DevTools Application > Service Workers에 활성 등록 확인 + 백엔드 죽이고 새로고침 → 셸 진입 OK.

### Stage 3 — OfflineIndicator + 갱신 알림 UX — 1 commit

- `web/src/components/OfflineIndicator.tsx` (`navigator.onLine` + 이벤트 listener + Tailwind banner).
- `web/src/lib/pwa-update.ts` (`useRegisterSW` 어댑터).
- `web/src/components/UpdatePrompt.tsx` (`needRefresh` toast + reload 버튼).
- `_authenticated/route.tsx` 또는 layout에 두 컴포넌트 결선.
- i18n 키 4종 (`pwa.offline.banner` / `pwa.update.available` / `pwa.update.reload` / `pwa.offline.mutationBlocked`) ko + en.
- 단위 테스트: `useIsOffline` hook + UpdatePrompt 렌더 (RTL).
- **검증**: vitest + 백엔드 단절 시 banner 표시 + 새 빌드 배포 시 toast.

### Stage 4 — mutation offline 가드 + 운영자 docs + handoff — 1 commit

- `web/src/api/hooks.ts` mutation hook들에 `useIsOffline` 가드 (선택 — D-PWA-4 결정 종속).
  - 옵션 A1: 모든 mutation hook에 `if (offline) throw new Error('offline')` (포괄).
  - 옵션 A2: hook은 그대로 두고 button-level disabled 패턴 (호출 site별).
- `docs/operations/pwa-deployment.md` 신규: SW 캐시 정책 + 갱신 절차 + 비상 시 빈 SW(unregister) 배포 + 디버깅 + 한계.
- `SESSION_HANDOFF.md` 진척 한 줄 + 결정 로그 D-PWA-1~8 채택 결과 한 줄.
- **검증**: vet + tsc + vitest + build + Go test 50+ 패키지 PASS, 회귀 0.

**총 ~1.5~2.0일** (Stage 1: 0.3일 — 아이콘 자산 작성, Stage 2: 0.3일, Stage 3: 0.5일, Stage 4: 0.4일).

### Stage 6 — (옵션 C 진입 trigger) react-query persist 추가

본 design doc 본 epic 비대상. customer feedback 기반 별 design doc 권장. trigger 시나리오:
- 첫 paying customer가 "오프라인 read 필수"라고 명시한 경우.
- 또는 어플라이언스 PoC 30일 운영 중 read 캐시 부재로 사용성 이슈 보고된 경우.

---

## 8. 결정 항목 (D-PWA-N)

각 항목 권장 default 명시 — 다음 세션 즉시 진입 부담 0.

### D-PWA-1 — service worker 모드 (generateSW vs injectManifest)

**선택지**:
1. **vite-plugin-pwa `generateSW` 모드** ← **권장 default**
2. vite-plugin-pwa `injectManifest` 모드 + 자체 `web/src/sw.ts`
3. 옵션 B 수동 SW (vite-plugin-pwa 미사용)

**근거**: generateSW가 Workbox 모범 사례 자동 적용 + 자체 SW 코드 0. injectManifest는 SW 동작 일부 customize 필요할 때만 (예: 백엔드 401 시 SW가 token refresh) — 본 epic 비대상. 회귀 위험 최소.

### D-PWA-2 — manifest.json 위치 + 정의 방식

**선택지**:
1. **vite-plugin-pwa 인라인 (vite.config.ts)** ← **권장 default**
2. `web/public/manifest.webmanifest` 정적 파일

**근거**: 인라인 방식은 build 시 자동 생성 + Vite hash 자산 url 자동 갱신. 정적 파일은 수동 관리 부담. 단, Stage 1에서는 정적 파일로 시작 후 Stage 2에서 plugin으로 이전 (gradient 도입).

### D-PWA-3 — 아이콘 자산 (192·512·apple-touch)

**선택지**:
1. **새로 디자인 (rosshield 로고 SVG → PNG 192/512/180)** ← **권장 default**
2. 기존 nrobotcheck 자산 답습
3. placeholder 아이콘 (제품 브랜드 D1 결정 후 갱신)

**근거**: D1(브랜드명) 미확정 상태이나 코드네임 `rosshield`는 확정. 단순 텍스트 + 방패 SVG로 placeholder 디자인 가능. PNG 변환은 `web/scripts/icons.sh` 또는 ImageMagick 1회 실행. 단, 디자이너 외부 작업 가능 시 Stage 1을 placeholder + Stage 6 final로 분리 가능.

### D-PWA-4 — mutation offline UX 차단 방식

**선택지**:
1. **button-level disabled + tooltip "오프라인 모드"** ← **권장 default**
2. 모든 mutation hook에 throw 가드 (전역)
3. 차단 없이 그대로 시도 → 자연 실패 + react-query error toast

**근거**: button-level은 사용자 의도 명확 + 추가 toast noise 0. 전역 throw는 hook 호출 site에서 캐치 처리 부담. 그대로 두기는 audit chain·leader epoch 등 관점에서 의도 불명확 mutation 시도가 백엔드 부활 시 의도치 않게 성공할 위험.

### D-PWA-5 — install prompt 노출 정책

**선택지**:
1. **브라우저 기본 prompt만 (커스텀 UI 없음)** ← **권장 default**
2. 첫 로그인 후 1회 toast로 "홈 화면에 추가" 안내
3. 설정 페이지에 "앱 설치" 버튼

**근거**: 브라우저 기본은 user gesture trigger + 표준 UX. 커스텀 prompt는 사용자 거부 시 다시 노출 정책·dismissed 상태 관리 부담. 본 epic은 installability 보장만 우선, 노출 UX는 후속 customer feedback 기반.

### D-PWA-6 — SW 갱신 정책 (autoUpdate vs prompt)

**선택지**:
1. **prompt (사용자 클릭으로 reload)** ← **권장 default**
2. autoUpdate (skipWaiting + clientsClaim 즉시)
3. silent (다음 새로고침까지 대기)

**근거**: prompt가 가장 안전 — 사용자가 작업 중 강제 reload로 데이터 손실 방지. autoUpdate는 audit 입력 폼 작성 중 사라질 수 있어 위험. silent는 사용자가 갱신 인지 못함 + stale 자산 장기 잔존 위험.

### D-PWA-7 — 인증 토큰 SW 접근 차단 + 캐시 분리

**선택지**:
1. **SW는 정적 자산만 캐시 + API/auth 모두 통과 (network-first 또는 SW 우회)** ← **권장 default**
2. SW가 GET API도 cacheFirst로 캐시 (단, /api/v1/auth/* + Authorization header 가진 요청 제외)
3. 옵션 C 채택 시 react-query persist만 사용, SW는 자산 전용

**근거**: SW가 Authorization Bearer 헤더 가진 응답을 캐시하면 사용자 A의 데이터가 SW 캐시에 남아 사용자 B 로그인 시 노출 위험. workbox `navigateFallbackDenylist: [/^\/api\//]`로 /api/* 는 SW 통과만 시키고 캐시 0. 옵션 C 진입 시 react-query persist 레이어가 메모리 + IndexedDB로 분리 관리 (이 경우 D-PWA-7-2 분리 정책 추가 필요).

### D-PWA-8 — 로그아웃 시 cache clear 정책

**선택지**:
1. **SW 캐시는 유지 + react-query 캐시는 logout시 clear (옵션 C 진입 시)** ← **권장 default**
2. 로그아웃 시 SW unregister + 모든 캐시 clear
3. 캐시 그대로 (다음 사용자가 stale 가능)

**근거**: SW 캐시는 정적 자산만 (사용자 별 데이터 0) → 로그아웃 무관. react-query persist는 사용자 데이터 → logout 시 `queryClient.clear()` + persist storage clear 필수. 옵션 A 단독에서는 react-query persist 부재 → 본 결정은 옵션 C 진입 시 활성.

---

## 9. 회귀 위험 / 운영 고려

### 9.1 Go embed 영향

- `//go:embed dist`는 dist 트리 전체 흡수 — 신규 manifest.webmanifest + sw.js + workbox-*.js 자동 포함. 변경 0.
- `Handler()`의 `/assets/*` immutable 1년은 SW에 무관 (SW 본체는 dist 루트). 단, Workbox가 생성하는 `workbox-*.js`는 hash 자산이라 immutable 안전.
- 회귀 테스트: `internal/web/embed_test.go`에 manifest + sw.js 존재 검증 추가 (~20줄).

### 9.2 SW 갱신 / rollback

- **갱신**: 새 빌드 배포 → 사용자 브라우저가 24h 또는 명시 reload 시 SW 갱신 fetch → 새 SW install → activate 대기.
- **prompt 정책 (D-PWA-6)**: 사용자 클릭으로 `updateSW(true)` 호출 → skipWaiting + reload. 사용자 작업 보호.
- **rollback 비상 절차**: 빈 SW (`self.addEventListener('install', () => self.skipWaiting()); self.addEventListener('activate', e => e.waitUntil(self.clients.claim()).then(() => self.registration.unregister()))`) 배포 → 사용자 브라우저에서 다음 reload 시 SW 제거. **운영자 docs §pwa-deployment.md에 본 절차 명시 필수**.

### 9.3 cosign keyless 서명 정적 자산 hash 일관

- vite-plugin-pwa는 build 시 dist에 새 파일 추가 (sw.js + workbox-*.js + manifest.webmanifest) → Go binary hash 변경.
- E26 release 파이프라인 cosign keyless 서명은 binary 단위라 영향 0.
- `audit verify` SDK는 audit chain hash만 검증 — 정적 자산 hash 무관.
- 회귀 0.

### 9.4 Customer dev 환경 SW 캐시 stale 디버깅

- dev 환경(`pnpm dev`)에서 SW 활성화는 `devOptions.enabled=false` 권장 (혼란 방지).
- 운영자/customer가 신규 SW 갱신 후 stale 자산 호소 시: DevTools > Application > Service Workers > Unregister + Clear storage → 강제 새로고침 절차 docs 명시.
- 한계: 사용자가 SW 자체를 모를 가능성 ↑ → install prompt UX 또는 docs 한 페이지 필수 (D-PWA-5 후속).

### 9.5 보안 — SW 동일 origin 권한 + 토큰 노출 방어

- SW는 same-origin 강제 (브라우저 표준) — 외부 origin fetch intercept 불가. P3 위반 0.
- **HttpOnly refresh cookie 보호 유지** — SW는 cookie 자동 동봉(브라우저 표준)하나 SW 코드에서 cookie 값 직접 read 불가 (HttpOnly).
- **Authorization Bearer 토큰**: SW가 fetch event handle 시 request.headers는 read 가능하나 캐시 대상 0 (D-PWA-7). 노출 위험 0.
- **XSS via SW**: SW 자체는 빌드 산출물 — 외부 입력 0. Workbox는 검증된 광범위 사용 라이브러리. 위험 ≈ 0.

### 9.6 멀티테넌시 — SW 캐시 tenant scope X

- SW 캐시는 origin 단위 (브라우저 표준) — tenant ID 별 분리 불가.
- SW는 정적 자산만 캐시 → tenant data 0 (D-PWA-7).
- 단, 옵션 C 진입 시 react-query persist는 tenant data 보유 → logout 시 clear + 다중 tenant SSO 시나리오에서 별도 분리 정책 필요 (별 design doc).

### 9.7 첫 paying customer 진입 영향

- **데모 신뢰성 ↑** — 데모 중 일시 단절에도 흰 화면 회피.
- **install 가능 = 모바일 브라우저 데모에서 "앱처럼 동작" 인상 ↑**.
- 어플라이언스 PoC 30일 운영 시 사용자 노트북 일시 단절 빈도 ↓ → 운영 incident 회피 기여.

### 9.8 한계 (의도적)

- 오프라인 mutation queueing 미실행 (audit chain 정합성 위험 — D-PWA-4 차단으로 우회).
- 오프라인 audit chain 검증 미실행 (`report verify` CLI 또는 Tauri 데스크톱 별 트랙).
- 오프라인 LLM advisor 미실행 (LLM 자체가 옵트인 + 별 트랙).
- iOS Safari install UX는 표준 manifest 외 추가 가이드 필요 (운영자 docs).

---

## 10. 참조

### 관련 design doc

- 직전 web epic: `internal/web/embed.go` (E10 Stage D, R12-11) — Go embed + Handler() SPA fallback 정책.
- `docs/design/01-principles.md` §3 (에어갭 1급) — 본 epic의 정책 근거.
- `docs/design/01-principles.md` §10 (프라이버시 기본값) — 로컬 우선.
- `docs/design/01-principles.md` §11 (설명 가능성) — 오프라인 fallback 안내 UX.
- `docs/design/01-principles.md` §12 (점진적 적용) — 옵션 A → 옵션 C 진화 권장.
- `docs/design/09-ui-and-clients.md` §9.2 — Web Console 스택 정의.
- `docs/design/11-tech-stack-and-roadmap.md` §11.8 (Tauri 데스크톱은 별 트랙 — D3 결정).
- `docs/design/phase5-backlog.md` — 본 epic 등재 위치 (PWA / 오프라인 지원 — vite-plugin-pwa).

### 코드 파일 (현재 상태)

- `web/package.json` (devDep 후보 위치)
- `web/vite.config.ts` (VitePWA plugin 등록 위치)
- `web/index.html` (manifest link 추가 위치)
- `web/public/` (현재 빈 — 아이콘 + favicon 신규)
- `web/src/main.tsx` + `web/src/App.tsx` (SW 등록 import 위치)
- `web/src/api/client.ts` (HttpOnly cookie + Bearer 헤더 정책)
- `web/src/api/hooks.ts:1241` (useScanProgress WebSocket — SW와 직교)
- `web/src/stores/auth.ts` (zustand persist accessToken)
- `internal/web/embed.go` (Go embed Handler — 변경 0 가설)
- `internal/web/embed_test.go` (회귀 테스트 추가 위치)

### 외부 참조 (최소)

- vite-plugin-pwa: <https://vite-pwa-org.netlify.app/> — generateSW vs injectManifest 모드 설명.
- Workbox 7: <https://developer.chrome.com/docs/workbox> — 캐시 전략 (precache·staleWhileRevalidate·cacheFirst·networkFirst).
- @tanstack/react-query-persist-client (옵션 C 진입 시): <https://tanstack.com/query/latest/docs/framework/react/plugins/persistQueryClient>.
- W3C Web App Manifest: <https://www.w3.org/TR/appmanifest/>.

### 메모리 패턴

- `feedback_design_doc_first.md` — 1.5~2.0일 작업이 본 design doc 우선 정책 적용 대상.
- `feedback_design_doc_conservative.md` — 추정 시간/효과 보수적 (옵션 A 1.5~2.0일은 아이콘 자산 작성 + Stage 4 docs 포함 보수치).
- `feedback_parallel_agents.md` — Stage 1(아이콘) + Stage 3(UX 컴포넌트)는 영역 분리로 sub-agent 병렬 가능.

### 결정 로그 후속 (epic 진입 시)

- D-PWA-1~8 채택 결과는 `SESSION_HANDOFF.md` 결정 로그에 한 줄 기록 (날짜 + 옵션 A 채택 + Stage 1 commit hash).
