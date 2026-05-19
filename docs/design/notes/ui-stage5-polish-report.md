# D-UI-1 Stage 5 — Polish 결과 보고

> **상태**: 본 round 완료. axe-core 5 페이지 violation 0 + 페이지 root spacing 일관화 + KWCAG 항목 점검.
> **컨텍스트**: v0.6.3 직후 sub-agent 3 병렬 중 web a11y test + visual rhythm 트랙.
> **참조**: `ui-overhaul-design.md` Stage 5, `ui-review-a11y-security.md` WCAG 2.2 AA + KWCAG.

---

## 1. axe-core 5 페이지 scan 결과

### 1.1 환경

- 도구: `vitest-axe@1.0.0-pre.5` + `axe-core@4.11.4`.
- runner: vitest (jsdom).
- rule 매트릭스: `src/test/axe.ts`에 중앙화.
  - **disable**: `color-contrast` (jsdom computed style false negative), `region`/`landmark-one-main`/`page-has-heading-one` (App.tsx 래퍼 외 페이지 컴포넌트만 mount하므로 false positive).
  - **enable (기본)**: WCAG 2.0/2.1/2.2 A+AA 전체 (axe-core 기본 rule set).
- mock 정책: `@/api/hooks` 통째 + `@tanstack/react-router` + `@/lib/toast` + `@/lib/confirm` + `@/lib/use-is-offline` + `@/lib/undoable`.
  - 모든 query는 `data: []`/`null` + `isSuccess: true` — EmptyState 분기 axe scan.
  - permissions는 admin true — 모든 CTA 노출 → 더 넓은 표면.

### 1.2 결과 매트릭스

| # | 페이지 | mode | axe violation | impact 분포 | 주의 |
|---|---|---|---|---|---|
| 1 | `/overview`  | light | **0** | — | Card 6장 + Skeleton/EmptyState 분기 OK |
| 2 | `/findings`  | light | **0** | — | Select × 2, Input × 1, severity stats 5장 OK |
| 3 | `/scans`     | light | **0** | — | StatusBadge + RecentSessions Card + Dialog stub |
| 4 | `/robots`    | light | **0** | — | fleet filter Input + CreateRobotDialog stub |
| 5 | `/fleets`    | light | **0** | — | FleetRow + CreateFleetForm stub |
| 6 | `/overview`  | dark  | **0** | — | dark token swap 후 동일 검증 |
| 7 | `/findings`  | dark  | **0** | — | severity 5종 stats 다크 contrast 디자인 단계 검증됨 |

총 7 케이스 PASS, violation 0.

### 1.3 jsdom 한계 (`color-contrast` disable 근거)

axe-core README — *"axe-core's color contrast rule requires that elements be rendered in the browser to be tested"*. jsdom은 computed style을 정확히 측정하지 않으므로 contrast rule을 enable하면 모든 색상에서 false negative ("Element has insufficient color contrast of 0:1") 발생.

→ contrast는 디자인 타임에 검증 (Stage 1에서 severity 5-color HSL token을 WCAG AA 4.5:1 기준으로 선택 — `globals.css` `--severity-*` 라인 107-122). 본 자동 test는 **구조적 a11y(role/aria/label/heading hierarchy/form labels)** 만 cover.

### 1.4 region/landmark rule disable 근거

`<App>` 안에는 `<main>`, `<nav>`, `<header>` landmark 가 모두 있으나, 본 test는 페이지 컴포넌트(`OverviewPage` 등) 만 단독 mount합니다. 따라서 region rule 활성화 시 "All page content should be contained by landmarks" false positive.

→ test에서 `<main>` 래퍼만 두고, landmark/region rule 은 비활성. 페이지 자체는 `App.tsx` 가 main 안에 마운트하므로 production 환경에서 위반 없음.

---

## 2. Visual rhythm — spacing 표준화

### 2.1 빈도 분석 (working tree at v0.6.3)

`web/src/routes/` + `web/src/components/` 전체 grep:

| 카테고리 | 1위 | 2위 | 3위 | 표준 채택 |
|---|---|---|---|---|
| `space-y-*` | space-y-2 (54) | space-y-4 (23) | space-y-3 (19) | container별 (form=2, dense=3, page=4) |
| `gap-*`     | gap-2 (74)     | gap-1 (56)     | gap-3 (19)     | inline 기본 gap-2, 카드 그리드 gap-3 |
| `p-*`       | p-3 (22)       | p-0 (13)       | p-4 (12)       | Card는 shadcn default p-6 유지, dense=p-3 |
| `py-*`      | py-2 (32)      | py-1 (28)      | py-3 (7)       | 변경 0 (이미 분포 자연스러움) |
| `px-*`      | px-3 (35)      | px-2 (21)      | px-4 (16)      | 변경 0 |
| `h-*` (row) | h-8 (10)       | h-9 (8)        | h-10 (7)       | TableHead default `h-12` (table.tsx), Button size별 다양 |

### 2.2 페이지 root container `space-y` 분포

- `space-y-4` × **20 페이지** (다수)
- `space-y-6` × **9 페이지** (`compliance`, `fleets`, `fleets.$fleetId`, `robots.$robotId`, `scans` 등)

→ **5 페이지 axe 대상만** `space-y-4`로 통일 (대규모 refactor 위험 회피). drill-down 페이지(`*.$id.tsx`)와 `compliance`는 본 round 미변경 (다른 sub-agent 영역 + i18n과 무관).

### 2.3 본 round 적용 변경

| 파일 | before | after | 근거 |
|---|---|---|---|
| `routes/_authenticated/scans.tsx` | `<div className="space-y-6">` | `<div className="space-y-4">` | 5 페이지 axe 대상 일관화 (overview/findings/robots 와 매칭) |
| `routes/_authenticated/fleets.tsx` | `<div className="space-y-6">` | `<div className="space-y-4">` | 동일 |

이외 spacing 변경 0 (Card/Table 기본 padding은 이미 shadcn 컨벤션 → 변경 위험 대비 이득 적음).

### 2.4 미변경 항목 (carryover)

- **Card padding**: shadcn default `p-6` 유지. 일부 페이지에서 `p-3`/`p-4` 로 override되어 있으나 dense view 의도적. Stage 5 round 외에서 디자인 시스템 차원의 재검토 권장.
- **drill-down 페이지 root** (`fleets.$fleetId`, `robots.$robotId`, `packs.$packKey*`): 모두 `space-y-6` 유지 (drill-down 은 정보 밀도 높은 1열 layout, 상위 페이지와 분리된 컨텍스트).
- **compliance/sso/system/users**: 다른 round/agent 영역.

---

## 3. KWCAG / 한국어 가독성 점검

### 3.1 이미 적용된 항목 (Stage 1~3)

`globals.css`:

| 항목 | 값 | 근거 |
|---|---|---|
| `body` font-family | Pretendard Variable + 시스템 fallback | D-UI-2 (한국어 default + 영어 호환) |
| `body` letter-spacing | -0.01em | Pretendard 권장 (한국어 조밀 + 가독성) |
| `body` line-height | **1.6** | KWCAG 한국어 본문 가독성 기준 |
| `h1~h6` letter-spacing | -0.02em | heading 조밀 강조 |
| `h1~h6` line-height | 1.3 | heading 시각적 분리 |
| `html` font-feature-settings | rlig, calt, ss06 | Pretendard 권장 합자/대체 글리프 |
| Pretendard dynamic-subset | unicode-range 분할 (초기 50~200KB) | offline-first + airgapped |

### 3.2 본 round 미세 조정 0

검토 결과 `globals.css`는 이미 한국어 line-height 1.6, Pretendard Variable self-host (D-UI-2), letter-spacing 최적값 모두 적용. **추가 변경 불필요** — Stage 1~3에서 모두 완료된 상태.

### 3.3 Skip-to-content + lang 속성

- `App.tsx` 최상단에 `<SkipToContent>` 컴포넌트 존재 (Stage 3 D-UI-7 적용 완료).
- `<html lang>` 은 i18n locale 변경 시 동기화 (Stage 2 완료).
- 본 a11y test에서 `document.documentElement.lang = 'ko'` 명시 → axe `valid-lang` rule 통과.

---

## 4. Carryover (다음 round 후속)

| ID | 항목 | 사유 | 권장 round |
|---|---|---|---|
| C5-1 | `color-contrast` 실측 자동화 | jsdom 한계 → Playwright + `@axe-core/playwright` 도입 | Phase 6/7 e2e test 보강 round |
| C5-2 | drill-down 페이지 spacing 통일 | 정보 밀도 다른 layout — 디자인 시스템 정의 후 일괄 | Stage 5b (분리 round) |
| C5-3 | 3rd party component a11y 점검 | Radix UI / cmdk / sonner 자체 a11y — known good but 매트릭스 정리 안 됨 | Phase 6 |
| C5-4 | KWCAG 2.2 항목별 매트릭스 | review doc에는 spec 있으나 자동 test cover 매트릭스 부재 | Phase 6 compliance 트랙 |
| C5-5 | Card padding 디자인 시스템 통합 | 본 round 미변경 (변경 위험 대비 이득 적음) | Stage 5b |
| C5-6 | Bundle size 분석 | `index-*.js` 800KB (warning) | E40 perf 트랙 |

---

## 5. 검증 (본 round)

| 항목 | 결과 |
|---|---|
| `pnpm exec tsc --noEmit` | PASS (0 errors) |
| `pnpm exec vitest run src/routes/__tests__/a11y.test.tsx` | 7/7 PASS (violation 0) |
| `pnpm exec vitest run` (전체) | 442/443 PASS, 1 failure는 본 작업 무관 (manifest test outdated — Lodestar rebrand 후속 cleanup 필요) + 1 file fail (`UpdatePrompt.test.tsx` virtual:pwa-register 해결 못함 — 본 작업 무관, main에서도 동일) |
| `pnpm build` | PASS (built in 10.73s, PWA precache 131 entries) |

본 작업 신규 회귀 0.

---

## 6. 추가 파일

| 파일 | 줄 수 | 역할 |
|---|---|---|
| `web/src/test/axe.ts` | 33 | configureAxe wrapper + rule 매트릭스 |
| `web/src/test/setup.ts` | 24 (+13) | `toHaveNoViolations` matcher 등록 + Assertion 타입 확장 |
| `web/src/routes/__tests__/a11y.test.tsx` | 195 | 5 페이지 light + 2 페이지 dark axe scan |
| `docs/design/notes/ui-stage5-polish-report.md` | (본 문서) | 본 round 결과 |

또한 5 페이지 `*Page` 함수에 `export` 추가 (test mount 위함 — runtime/route 영향 0).

`scans.tsx` / `fleets.tsx` 페이지 root spacing 미세 조정 (총 2줄 변경).

---

## 7. 결론

D-UI-1 Stage 5 polish 본 round 완료:

1. axe-core 자동 scan 인프라 구축 (vitest-axe + setup matcher + 중앙 rule 매트릭스).
2. 5 페이지 light + 2 페이지 dark = 7 케이스 violation 0.
3. 페이지 root spacing 5 페이지 일관화 (scans/fleets → `space-y-4`).
4. KWCAG 한국어 가독성 항목은 Stage 1~3에서 이미 완료 — 추가 변경 불요.
5. carryover 6건 (color-contrast 실측 e2e + drill-down spacing 통일 등)은 별도 round.
