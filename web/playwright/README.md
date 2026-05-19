# Playwright E2E (C4 scaffold)

> rosshield Web Console의 end-to-end smoke 테스트.
> Phase 3 — Carryover C4 회수 1차 스캐폴드. 깊은 단정은 후속 epic에서 추가.

## 무엇이 있는지

```
web/playwright/
├─ README.md                  # 이 문서
├─ fixtures.ts                # admin 계정, ko/en 라벨 상수
├─ helpers.ts                 # loginAsAdmin, resetClientState
├─ global-setup.ts            # go build → seed admin/demo → 백그라운드 부팅
├─ global-teardown.ts         # 서버 SIGTERM + tmp dataDir 삭제
└─ tests/
   ├─ auth.spec.ts            # 로그인 → 보호 라우트 → 로그아웃
   ├─ robots.spec.ts          # /robots — seed demo robot 노출 + 등록 폼 토글
   ├─ compliance.spec.ts      # /compliance — ISMS-P 프로필 추가
   ├─ audit.spec.ts           # /audit — ChainHead seq + hash 노출
   ├─ i18n.spec.ts            # 헤더 Globe 토글 → en 라벨 노출
   ├─ theme.spec.ts           # 헤더 sun/moon/monitor 토글 → .dark 클래스 변화
   └─ color-contrast.spec.ts  # 5 페이지 × light/dark = 10 케이스 WCAG AA 실측
```

`web/playwright.config.ts`는:

- `globalSetup` / `globalTeardown` 활성.
- `workers: 1` — 단일 sqlite dataDir 격리.
- `baseURL = http://127.0.0.1:8123` (E2E_BACKEND_PORT로 override).
- HTML reporter는 CI에서만 활성 (로컬은 list).

## 로컬 실행

### 사전 조건

1. **Go 1.26**, **Node 22**, **pnpm 10**.
2. **Web 빌드 결과** — `internal/web/dist/index.html`이 있어야 함.
   ```bash
   cd web && pnpm install && pnpm build
   ```
3. **Playwright 브라우저** — 첫 실행 전 한 번:
   ```bash
   cd web && pnpm exec playwright install chromium
   ```

### 실행

```bash
# 모든 spec 실행 (headless chromium).
cd web
pnpm exec playwright test

# UI 모드 (debug용).
pnpm exec playwright test --ui

# 특정 spec만.
pnpm exec playwright test playwright/tests/auth.spec.ts

# 실패 시 dataDir 보존 (forensics).
PLAYWRIGHT_E2E_KEEP_DATA=1 pnpm exec playwright test
```

### 설정 가능한 환경 변수

| 변수 | 기본값 | 용도 |
|---|---|---|
| `E2E_BACKEND_PORT` | `8123` | rosshield-server bind port |
| `E2E_BACKEND_URL` | `http://127.0.0.1:<PORT>` | playwright baseURL override |
| `PLAYWRIGHT_E2E_KEEP_DATA` | unset | `1` 시 tmp dataDir 보존 |

## globalSetup 흐름

1. `internal/web/dist/index.html` 존재 확인 — 없으면 즉시 throw.
2. tmpdir에 격리 dataDir 생성 (`rosshield-e2e-XXXXXX`).
3. `go build -o bin/rosshield-server-e2e[.exe] ./cmd/rosshield-server`.
4. `seed admin --email e2e-admin@example.com --password-stdin`.
5. `seed demo --email e2e-admin@example.com`.
6. 백그라운드로 서버 spawn → `:8123/healthz` 폴링 (최대 30초).
7. `globalThis.__ROSSHIELD_E2E__`에 `{ server, dataDir, binPath }` 보관.

`globalTeardown`은:

- `server.kill('SIGTERM')` + 5초 후 SIGKILL fallback.
- dataDir 삭제 (`PLAYWRIGHT_E2E_KEEP_DATA=1`이면 보존).

## 한계 (후속 항목)

- **단일 worker** — sqlite dataDir 공유 때문. 병렬 실행 시 고려: 테스트당 별 dataDir + 별 포트.
- **snapshot 흐름 미검증** — compliance.spec은 프로필 추가까지만. session ID를 가져다 snapshot 생성·ScoreHero/ControlsBreakdown 노출 검증은 후속.
- **Findings·Advisor·Reports 페이지** — 본 스캐폴드 미포함. 도메인 로직이 더 무거우므로 epic 단위 후속.
- **WebSocket scan progress** — C1 epic의 /scans 진행률 스트림은 별도 spec 필요.
- **Tauri 데스크톱 앱** — 본 설정은 web 모드만. Tauri는 `@tauri-apps/cli` 기반 별도 harness.
- **시각 회귀** — 스크린샷 baseline 없음. 추후 Percy/Argos 도입 검토.
- **i18n 영구 상태** — i18n.spec은 토글 후 cleanup이 store에 남을 수 있음. 각 spec이 `resetClientState`로 격리.

## CI 통합

`.github/workflows/ci.yml`에 `e2e` job이 추가됐다:

- Node 22 + pnpm 10 + Go 1.26.
- `pnpm install` → `pnpm build` (web).
- `pnpm exec playwright install --with-deps chromium`.
- `pnpm exec playwright test`.
- 실패 시 `playwright/playwright-report/` HTML 리포트를 artifact로 업로드.

## color-contrast 실측 (Stage 5b)

`color-contrast.spec.ts`는 axe-core의 color-contrast rule을 실 chromium에서 평가한다.
jsdom 기반 vitest-axe는 computed style 한계로 contrast rule을 비활성하지만(`web/src/test/axe.ts`),
실 브라우저는 정확한 픽셀 측정이 가능하므로 WCAG 2.2 AA 4.5:1을 실측한다.

### 실행

```bash
cd web
pnpm exec playwright test playwright/tests/color-contrast.spec.ts
```

5 페이지(`/overview`, `/findings`, `/scans`, `/robots`, `/fleets`) × light/dark 2 모드
= 총 10 케이스.

### 추가 의존성

- `@axe-core/playwright` — devDep로 설치 (Playwright fixture에 axe scan 주입).

### 다크 모드 강제 적용

Playwright의 `colorScheme: 'dark'` project를 분리하지 않고, helper
`applyThemeMode(page, 'dark')`로 `html.dark` 클래스를 직접 토글한다.
근거:
- rosshield는 `prefers-color-scheme` 자동 감지가 아니라 zustand persist + DOM 클래스 모델.
- spec 안에서 모드를 토글하면 페이지 reload 없이도 light/dark 두 측정이 가능.

### 위반 발견 시

`results.violations` JSON이 spec stdout에 출력된다:

```json
[{ "id": "color-contrast", "impact": "serious",
   "nodes": [{ "target": ["button.outline"],
                "failureSummary": "Fix any of the following: ..." }] }]
```

→ 해당 컴포넌트의 색상 token 또는 className을 수정한다. `globals.css`의 `--*` HSL token
또는 컴포넌트별 `text-muted-foreground` 등 utility class가 1차 의심.

## 트러블슈팅

| 증상 | 원인 | 조치 |
|---|---|---|
| `Web build artifact missing` throw | `internal/web/dist`가 비어있음 | `cd web && pnpm build` |
| `seed admin: ... refusing duplicate seed` | dataDir이 보존돼 있음 | `PLAYWRIGHT_E2E_KEEP_DATA` 미사용 또는 tmp 정리 |
| `server did not become healthy` | 포트 충돌 / build 실패 | `lsof -i :8123` / `go build ./cmd/rosshield-server` 단독 실행 |
| Playwright 브라우저 없음 | 첫 실행 | `pnpm exec playwright install chromium` |
