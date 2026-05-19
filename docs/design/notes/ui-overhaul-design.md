# UI/UX 전면 개선 — D-UI-1 통합 설계 (2026-05-19)

> **상태**: 4 페르소나 review (`ui-review-{visual,ux,frontend,a11y-security}.md`) 직후 사용자 명시 요청("UI/UX 전면적으로 개선해 주세요. 단순 CSS 수정 수준이 아니라 사용자가 실제로 쓰기 편한 정보 구조와 컴포넌트 구조로 리팩토링"). 본 doc은 **Stage 1+2+3 진행 계획 + Stage 4+5 carryover**.
>
> **R 식별자**: R-UI-OVERHAUL (master). Stage 1~5.
>
> **참조**:
> - `docs/design/notes/ui-review-visual.md` (5.5/10, severity 색상 + 브랜드)
> - `docs/design/notes/ui-review-ux.md` (5.7/10, IA 15 flat → 4 그룹)
> - `docs/design/notes/ui-review-frontend.md` (6.0/10, Toast + Form + Skeleton)
> - `docs/design/notes/ui-review-a11y-security.md` (WCAG 6.5/10, Skip-to-content)
> - `web/src/components/ui/` (shadcn/ui 20개 컴포넌트, Tailwind 4.0)
> - `web/src/routes/_authenticated/` (20 페이지)

---

## 1. 목표 & 비목표

### 1.1 목표 (사용자 spec 정확 인용)

1. **전체적인 인상** — 관리자용 웹 대시보드처럼 깔끔하고 신뢰감 있는 디자인, 여백·정렬·폰트·버튼 정돈, 실무자 장시간 사용 시 피로 0
2. **레이아웃** — 정보 구조 재배치, Header·Sidebar·본문 역할 명확, 목록/상세/등록/수정 일관 패턴, 반응형
3. **UX** — CTA 명확, 폼 라벨/도움말/오류, empty/loading/error 자연스러움, destructive 확인·피드백
4. **디자인 시스템** — color·button·card·table·form·modal·badge 일관, Tailwind 재사용 컴포넌트, shadcn/ui 참고
5. **유지할 것** — 기존 기능·API 연동 유지, 라우팅·데이터 흐름 변경 0, 비즈니스 로직 변경 0

### 1.2 비목표

- **기능 추가 0** (라우트·API hook·도메인 로직 변경 0)
- **새 라이브러리 무리 추가 0** (sonner + react-hook-form + zod + @tanstack/react-table 4개만 P0)
- **rebrand 0** (Lodestar 브랜드명만 적용, 로고 디자인은 별 round)

---

## 2. 옵션 비교 (≥3, design doc 우선 정책 일관)

| 옵션 | 접근 | Stage | 추정 | 강점 | 약점 |
|---|---|---|---|---|---|
| **A** | **Stage 1+2+3 본 round + Stage 4 다음 round + Stage 5 마지막 polish** | 5 | 본 round ~30분 + 다음 round ~수 시간 + polish | 컨텍스트 안전 + 단계적 검증 + 메인 부담 적음 | 사용자 다음 round 트리거 필요 |
| B | 모든 Stage 1~5 본 round + sub-agent 5~7 병렬 | 5 | 본 round 1~2h | 한 round에 끝 | cherry-pick 충돌 large + 메인 컨텍스트 한도 risk |
| C | 통합 backlog markdown만 + 사용자가 우선순위 결정 후 진행 | 1 | 30분 | 사용자 통제 최대 | 진행 지연 |

### 권장: 옵션 A

- Stage 1+2+3 = design system + shared component + layout (기반 구조)
- Stage 4 = 20 페이지 일관 적용 (mechanical, sub-agent 5 병렬 가능)
- Stage 5 = polish (a11y 측정 + visual finetune)
- 사용자가 Stage 1+2+3 결과 보고 Stage 4 진행 결정

---

## 3. Stage 분해 (옵션 A)

### Stage 1 — Design System Token (sub-agent A)

**위치**: `web/tailwind.config.ts` + `web/src/styles/tokens.css` (신규) + `web/src/lib/severity.ts` (신규)

**산출**:
- **Severity 5-color HSL token** (light + dark): `--severity-critical`, `--severity-high`, `--severity-medium`, `--severity-low`, `--severity-info` — 모두 WCAG AA 4.5:1 검증
- **Status 5-color**: `--status-running`, `--status-pending`, `--status-completed`, `--status-failed`, `--status-cancelled`
- **Typography scale**: text-xs(11) ~ text-3xl(30), letter-spacing, line-height
- **Spacing scale**: Tailwind default 보존 + section-y, card-padding 등 semantic alias
- **Font family**: Pretendard Variable (한국어 최적 + 영어 호환, self-host npm package)
- **Tailwind 4.0 @theme inline** 활용 (v4 신문법)

**검증**:
- 색상 대비 측정 (a11y review의 수치 기준)
- 다크 모드 token 모두 추가

### Stage 2 — Shared Component (sub-agent B)

**위치**: `web/src/components/ui/` + `web/src/components/common/` (신규)

**신규**:
1. **Toast** — sonner dep 추가 + `<Toaster />` provider in `App.tsx` + `web/src/lib/toast.ts` wrapper (success/error/info/warning)
2. **AlertDialog** — shadcn/ui alert-dialog.tsx (이미 있음) + `web/src/lib/confirm.ts` (window.confirm 대체)
3. **Skeleton** — `web/src/components/ui/skeleton.tsx` + 페이지/카드/테이블 row variant
4. **EmptyState** — 기존 `layout/EmptyState.tsx` 보강 + variant (no-data · no-permission · loading-fail) + CTA 슬롯
5. **SeverityBadge** — `web/src/components/common/SeverityBadge.tsx` (severity 5-color token + icon Lucide ShieldAlert · AlertCircle 등)
6. **StatusBadge** — `web/src/components/common/StatusBadge.tsx` (status 5-color token + animated dot)
7. **Form 표준** — react-hook-form + zod + @hookform/resolvers dep + `web/src/components/ui/form.tsx` (shadcn 표준 — FormField + FormLabel + FormControl + FormDescription + FormMessage)
8. **PageHeader 보강** — `layout/PageHeader.tsx` (title + subtitle + badge + actions + breadcrumbs) 일관 패턴

**dep 추가**:
- `sonner` (toast)
- `react-hook-form` + `zod` + `@hookform/resolvers`
- `pretendard` (npm package, self-host)

### Stage 3 — Layout 재구성 (sub-agent C)

**위치**: `web/src/components/layout/Sidebar.tsx` + `Header.tsx` + `web/src/App.tsx` + Breadcrumbs

**변경**:
1. **Sidebar 4 그룹** (UX review 권장):
   - **운영** — Overview · Scans · Findings · Robots · Fleets
   - **컴플라이언스** — Compliance · Reports · Audit · Packs
   - **지능화** — Advisor (LLM, opt-in 표시)
   - **관리** — Integrations(+SSO) · Users · System(+License) · Settings
2. **Sidebar collapse** — desktop은 toggle, mobile은 drawer (Vaul or shadcn Sheet)
3. **Header** — Lodestar 브랜드 (text logo) + 현재 tenant 명 + 현재 user role badge (admin·operator·auditor 색상 분리) + theme toggle (light/dark) + user dropdown (logout)
4. **Skip-to-content link** — `App.tsx` 최상단, sr-only + focus visible 시 노출 (KWCAG/공공 입찰 요구)
5. **Breadcrumbs 일관** — 모든 drill-down 페이지(`/robots/:id`, `/packs/:key`, `/packs/:key/checks/:id`, `/fleets/:id`) 자동 노출
6. **`<html lang>` 동적 update** — i18n locale 변경 시 동기화
7. **Mobile breakpoint** — sidebar hamburger + 모든 페이지 horizontal scroll 0

### Stage 4 — 20 페이지 일관 패턴 적용 (carryover, 다음 round)

20 페이지 sub-agent 5 병렬 (각 4 페이지) 또는 메인 일괄. PageHeader · Toast · AlertDialog · EmptyState · SeverityBadge · StatusBadge · 모바일 반응형 일관 적용.

### Stage 5 — Polish (carryover, 마지막 round)

a11y axe-core 측정 + visual rhythm finetune + 다크 모드 검증.

---

## 4. 결정 항목 (Stage 1+2+3 권장 default)

| ID | 결정 | 권장 default | 근거 |
|---|---|---|---|
| D-UI-1 | Severity color palette | Tremor severity scale 차용 (critical=#dc2626 / high=#ea580c / medium=#ca8a04 / low=#2563eb / info=#0891b2, 다크 lighter variant) | WCAG AA 검증 가능 + color blind 구분 |
| D-UI-2 | Font family | Pretendard Variable self-host | 한국어 default + 영어 호환 + npm package |
| D-UI-3 | Toast library | sonner | shadcn 표준 + 가벼움(7KB) |
| D-UI-4 | Form validation | react-hook-form + zod + @hookform/resolvers | shadcn 표준 + 타입 안전 |
| D-UI-5 | Sidebar 그룹 | 4 그룹 (운영/컴플라이언스/지능화/관리) | UX review §4 |
| D-UI-6 | Mobile drawer | shadcn Sheet (이미 dep) | 신규 dep 0 |
| D-UI-7 | Dark mode | Stage 3에서 token 모두 cover, theme toggle UI 신규 | a11y review P0 + Tremor 등 모던 SaaS 표준 |

---

## 5. 검증

- 코어 빌드 영향 0 (web 변경만)
- vitest 159 PASS 유지 (회귀 0)
- Playwright E2E PASS 유지
- 빌드 결과 `pnpm build` PASS (bundle 분석은 Stage 5)
- 메인 페이지 시각 검증 (사용자 로컬 brower)

---

## 6. 비즈니스 로직 / API 영향 — 0

- 라우팅 구조 변경 0
- API hook 변경 0
- 도메인 model 변경 0
- 마이그레이션 0

Stage 1+2+3 모두 web/src/ 안에서만 작업.

---

## 7. Stage 4+5 carryover 명시

본 doc은 Stage 4+5 spec 미확정. Stage 1+2+3 cherry-pick + 사용자 사용 후 다음 round 진입 시점에 Stage 4 spec 확정(20 페이지 mapping + 각 sub-agent 분담).
