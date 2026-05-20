# D-UI-1 Stage 5b additional — 잔여 페이지 4건 axe scan 보고

> **상태**: ✅ 본 round 완료 (8 신규 케이스 PASS, violation 0).
> **컨텍스트**: Lodestar v0.6.6 직후 sub-agent 3 병렬 중 1 (web a11y 트랙).
> **참조**:
> - `ui-stage5b-color-contrast-report.md` §6 carryover C5b-4 (Login/Invitation accept) + C5b-5 (Settings/Users/System)
> - 기존 5 페이지 a11y scan: `web/src/routes/__tests__/a11y.test.tsx` (Overview/Findings/Scans/Robots/Fleets)
> - jsdom 기반 vitest-axe (`web/src/test/axe.ts`) — color-contrast rule disable 정책

---

## 1. 배경

Stage 5에서 5 핵심 페이지 axe scan + Stage 5b color-contrast (playwright spec) 도입 후,
carryover 2건이 남아 있었다:

- **C5b-4** — 인증 전 페이지 (Login, Invitation accept) 는 `loginAsAdmin` helper 의존이
  없어 별 round로 분리.
- **C5b-5** — admin role + form-heavy 페이지 (Settings, Users, System) 는 RBAC mock +
  form/table widget 다양성으로 별 round로 분리.

본 round는 두 carryover를 단일 work-unit으로 묶어 처리. 결과: **9 페이지 누적 cover**.

---

## 2. 추가 (본 round)

### 2.1 신규 파일

| 파일 | 줄 수 | 역할 |
|---|---|---|
| `web/src/routes/__tests__/a11y-additional.test.tsx` | 297 | Login + Invitation accept + Settings + Users + System axe scan (light + dark) |
| `docs/design/notes/ui-stage5b-additional-report.md` | (본 문서) | 본 round 결과 |

### 2.2 수정 파일 (named export 추가)

기존 Stage 5 round 패턴(overview/findings/scans/robots/fleets의 `*Page` named export)을
잔여 4 페이지에 동일 적용:

| 파일 | 변경 | 근거 |
|---|---|---|
| `web/src/routes/login.tsx` | `function LoginPage` → `export function LoginPage` (+5줄 주석) | 테스트 mount용 |
| `web/src/routes/invitations.accept.$token.tsx` | `AcceptInvitationPage`(route용) + `export function AcceptInvitationView({ token })` 분리 (+10줄) | route param 의존 분리 → 테스트에서 token prop으로 mount |
| `web/src/routes/_authenticated/settings.tsx` | `function SettingsPage` → `export function SettingsPage` (+2줄 주석) | 테스트 mount용 |
| `web/src/routes/_authenticated/users.tsx` | `function UsersPage` → `export function UsersPage` (+2줄 주석) | 테스트 mount용 |
| `web/src/routes/_authenticated/system.tsx` | `function SystemPage` → `export function SystemPage` (+1줄 주석) | 테스트 mount용 |

**핵심 분리 결정** — AcceptInvitationPage는 `Route.useParams()`로 token을 추출하는데,
테스트에서 `createFileRoute`를 mock하면 Route 객체에 `useParams` 메소드가 없다. inner
view를 별 named export(`AcceptInvitationView({ token })`)로 분리해 router 의존 0으로
mount 가능.

### 2.3 axe scan 매트릭스

각 페이지를 jsdom + vitest-axe로 mount → `axe(container)` 호출.

| 페이지 | light mode | dark mode | violation |
|---|---|---|---|
| Login | ✅ PASS | ✅ PASS | 0 |
| Invitation accept (active form 분기) | ✅ PASS | — (생략) | 0 |
| Settings | ✅ PASS | ✅ PASS | 0 |
| Users | ✅ PASS | ✅ PASS | 0 |
| System | ✅ PASS | — (생략) | 0 |

**총 8 케이스 (5 light + 3 dark) PASS, violation 0.**

dark mode 샘플링 결정 — 모든 페이지가 동일한 design token(`web/src/index.css`의 `.dark`
selector) 을 사용하므로 Login(인증 전 surface) + Settings(card-heavy admin) + Users
(form + table) 3건으로 dark token cross-application을 충분히 cover. Invitation accept
와 System은 동일 token 재사용이라 light scan으로 충분 (System은 Healthz fetch mock 부담
도 고려).

---

## 3. 설계 결정

### 3.1 단일 파일 vs 별 파일 분리

| 방안 | 장점 | 단점 | 채택 |
|---|---|---|---|
| `a11y.test.tsx` 확장 | 단일 진입점, mock 재사용 | mock dict 비대, useMe/useLicenseInfo 등 신규 hook stub 추가 → 기존 5 페이지 회귀 risk | ❌ |
| `a11y-additional.test.tsx` 신규 | 신규 mock(useMe·useLicenseInfo·useBackups·useUsageStats·useScans·useInvitations·useInvitationByToken·useLogin·useAcceptInvitation·useCreateInvitation·useDeleteInvitation) 격리, 기존 5 페이지 회귀 risk 0 | 파일 2개 | ✅ |

→ **별 파일 채택**. 기존 7 케이스 전부 PASS 유지 (회귀 0).

### 3.2 API hooks mock 전략

`@/api/hooks` 모듈 통째 stub — 페이지가 의존하는 query/mutation 만 선언적으로 빈 success
상태 또는 1행 데이터로 제공. 본 round 신규 mock:

- **Queries**: useMe, useLicenseInfo, useBackups, useUsageStats, useScans, useInvitations,
  useInvitationByToken
- **Mutations**: useLogin, useAcceptInvitation, useCreateInvitation, useDeleteInvitation
- **RBAC**: useHasPermission = true (모든 admin CTA · table action 노출 → 더 넓은 axe scan
  표면)
- **선택적 mock**: `@/lib/route-guards.requirePermission` (createFileRoute mock으로 호출되지
  않지만 import 해소용), `@/api/client.API_BASE_PATH` (System 페이지의 BackupRow 다운로드
  URL 생성)

### 3.3 healthz fetch stub

System 페이지의 `useHealthz`는 `useQuery + fetch('/healthz')` 패턴이라 모듈 mock으로
가로채기 어려움. `beforeEach`에서 `vi.stubGlobal('fetch', ...)` 로 `ok: true, status: 200`
+ healthzResponse JSON 반환하도록 stub. `afterEach`에서 `vi.unstubAllGlobals()`.

### 3.4 Invitation accept 페이지 — 어느 분기를 cover?

- **active**: form 노출 (3 input + submit) → 가장 a11y 표면이 넓음 → **cover**
- expired/used/notfound: 카드 + 안내 텍스트 only — 분기는 단순하고 form 0 → 분리 cover 안 함
  (carryover로도 분리 안 함, 동일 token 사용 + 단순 layout)

active 분기 mock — `useInvitationByToken` 이 `{ email, roleName, expiresAt, accepted: false }`
반환 → preview.isPending=false, isSuccess=true, data 존재 → AcceptForm 렌더 분기.

### 3.5 color-contrast rule 유지 (disable)

기존 `web/src/test/axe.ts` 정책 그대로 — jsdom의 computed style false negative 회피.
color-contrast 실측은 Stage 5b playwright spec(`web/playwright/tests/color-contrast.spec.ts`)
에서 담당 (carryover C5b-1 — 통합 빌드 직후 실 실행).

---

## 4. 테스트

| 항목 | 결과 |
|---|---|
| `pnpm exec tsc --noEmit` | ✅ PASS (0 errors) |
| `pnpm exec vitest run src/routes/__tests__/` | ✅ 15 PASS (기존 7 + 신규 8) |
| `pnpm exec vitest run` 전체 | 454 PASS, 1 pre-existing fail (manifest brand) + 1 pre-existing import error (UpdatePrompt virtual:pwa-register) — **본 task 무관 회귀 0** |
| `pnpm build` | ✅ PASS (126 entries, 3997 KiB precache) |

**사전 실패 확인** — `git stash` 후 baseline에서도 동일하게 두 케이스 실패. 본 round에서
도입된 회귀 0.

---

## 5. 누적 cover 매트릭스

| Round | 페이지 | violation | 비고 |
|---|---|---|---|
| Stage 5 | Overview · Findings · Scans · Robots · Fleets | 0 | `a11y.test.tsx`, 7 케이스 (5 light + 2 dark) |
| Stage 5b additional (본 round) | Login · Invitation accept · Settings · Users · System | 0 | `a11y-additional.test.tsx`, 8 케이스 (5 light + 3 dark) |

**누적 = 9 페이지 cover, violation 0, 15 케이스 PASS.**

미cover 영역 (drill-down + 인증 외 부) — carryover §6 참조.

---

## 6. carryover (다음 round)

| ID | 항목 | 사유 | 권장 round |
|---|---|---|---|
| C5b-3 | drill-down 페이지 (`fleets.$id`, `robots.$id`, `packs.$pack.checks.$check`, `audit`, `compliance`, `reports`, `sso`, `license`, `advisor`, `integrations`) | URL param seed + 추가 hook stub 필요. 본 round 범위 초과 | 별 round 또는 통합 e2e 후 |
| C5b-6 | Invitation accept expired/used/notfound 분기 | 본 round는 active form 분기만. 단순 카드 layout이라 violation risk 낮음 | drill-down 묶어서 또는 회귀 발견 시만 |
| C5b-7 | 3rd party widget (sonner toast portal, Radix Dialog 열린 상태, Recharts chart svg, monaco editor) | portal/측정 시점 차이 + 외부 lib 신뢰. axe scan 별 setup 필요 | 별 epic 또는 Storybook 도입 시 |
| C5b-8 | 키보드 navigation 자동 cover (tab order, focus trap) | axe scan 영역 외 — vitest-keyboard 또는 playwright keyboard fixture 도입 필요 | E40 perf/CI 트랙 |
| C5b-9 | Login 에러 분기 (ApiError isUnauthorized) + Users 초대 생성 후 토큰 카드 노출 분기 | 본 round는 초기 mount만. 인터랙션 후 분기 cover는 별 round | UX deep-test 시 |

---

## 7. 한계

- **jsdom 한계** — color-contrast, layout shift, focus 시각 cue 등은 실 브라우저 픽셀이
  필요. 본 round는 role/aria/label/landmark/heading-order 등 markup 레벨만 cover. 실측은
  Stage 5b playwright spec(C5b-1) 담당.
- **인터랙션 미cover** — 모든 페이지를 초기 mount 상태로만 scan. 모달 열림, 폼 에러 표시,
  toast 노출 후의 상태는 cover 안 됨 (C5b-9 carryover).
- **drill-down 페이지 미cover** — top-level 9 페이지만. detail/edit 페이지는 별 round 필요
  (C5b-3 carryover).
- **i18n locale 미cover** — 모든 scan은 `lang="ko"`. 영어 locale에서 텍스트 길이 차이로
  layout overflow 등 risk는 별 visual regression 도구 필요.

---

## 8. 결론

- **잔여 페이지 4건 axe scan 추가 완료** (Login + Invitation accept + Settings + Users +
  System — 5 페이지 5 case + 3 dark mode = 8 PASS, violation 0).
- 누적 cover **9 페이지** (Stage 5 5 페이지 + 본 round 5 페이지 — Invitation accept를 1
  페이지로 셈하여 9, 또는 dark mode 별도 counting 시 15 케이스).
- carryover C5b-4 + C5b-5 closed.
- 기존 a11y violation 0 — 본 round에서 fix 필요한 항목 0 (페이지가 이미 Stage 4 표준화
  과정에서 PageHeader · Label · aria-label · role · landmark 등 정비됨).
- 본 round 회귀 0 (사전 존재 manifest brand + UpdatePrompt virtual:pwa-register 실패는
  무관).
