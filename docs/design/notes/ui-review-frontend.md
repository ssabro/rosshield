# UI/UX Review — Frontend Engineering / Interaction Design 관점 (2026-05-19)

> 페르소나: 시니어 Frontend Engineer (10년+, React/TS/Tailwind, Linear · Vercel · Sentry 류 modern web app 전문).
> 대상: `web/` (rosshield Web Console). 본 review는 4 페르소나 sub-agent 중 **Frontend Engineering** 영역 (Visual / UX research / Accessibility는 별 sub-agent).

---

## 1. 종합 평가

**점수**: **6.0 / 10** — 견고한 기초, 그러나 **interaction polish 부족**으로 enterprise SaaS 기대치(Linear/Vercel)와 큰 gap.

한 줄 요약: shadcn/ui · TanStack · Zustand 스택 선택은 modern·검증된 정공법이지만, **Toast / Form 검증 / Table 고급 기능 / Mobile 대응 / Optimistic update**가 모두 부재해 "기능은 다 되는데 손에 익으면 답답한" 1세대 admin tool 수준에 머물러 있다. 보안 감사 도구는 admin 사용자가 하루 종일 보는 화면이므로 본 영역들 polish가 dev velocity 대비 ROI 1위.

**비교**:
- Linear/Vercel: cmd+K · sonner toast · row action menu · optimistic mutation · skeleton 기본
- 현재 console: window.confirm 6건 · window.prompt 1건 · `<p className="text-destructive">` inline error 17+건 · skeleton 0건 · mobile sidebar 0건

---

## 2. 기술 스택 평가

**선택**: React 19 + TypeScript 5.7 + Vite 6 + TanStack Router v1 + TanStack Query v5 + Zustand 5 + Tailwind 4 + shadcn/ui (Radix).

**적절성**: ★★★★★ — 2025-2026 신규 B2B SaaS startup이라면 거의 같은 스택 선택. React 19는 server component 도입 전이라 SPA + TanStack Router 조합으로 file-based routing + code splitting 자동 (vite.config.ts `autoCodeSplitting: true`) — best in class.

**선택의 미세 우려**:
- React 19 사용 확인됨 (package.json `^19.0.0`) 그러나 **React 19 신기능 활용 0건** (`Suspense`, `useTransition`, `useDeferredValue`, `use()`, `useOptimistic`, `useActionState` 0회) — 큰 ROI 기회 손실. 특히 `useOptimistic`은 mutation 패턴에 직접 적용 가능.
- React 19 + TanStack Router 조합이 stable production 사례 적은 편 — 다만 본 codebase는 이미 안정 운영 중이라 위험 0.

**부족한 dep / 권장 추가**:

| dep | 용도 | 우선순위 |
|---|---|---|
| **sonner** | Toast notification (success / error / loading / promise) | P0 |
| **react-hook-form** + **zod** + **@hookform/resolvers** | Form 검증 표준화 (현재 controlled state + 수동 검증) | P0 |
| **@tanstack/react-table** | sortable / filterable / paginated / virtualized table | P1 |
| **@tanstack/react-virtual** | 1000+ row table virtual scroll (table 미사용 시 단독) | P2 |
| **vite-bundle-visualizer** (dev only) | bundle 분석 — 현재 main 340KB · utils 149KB · select 51KB | P2 |
| **vaul** (mobile drawer) 또는 **@radix-ui/react-dialog** + side-aware sheet 패턴 | mobile sidebar drawer | P1 |

---

## 3. 강점 (Strengths)

1. **shadcn/ui adoption 일관성 ★★★★** — 23개 ui primitive 모두 shadcn 표준 (Radix + cva + forwardRef + cn). Button `asChild` polymorphic 패턴 정착 (`<Button asChild><Link ...>`). Compound component 패턴 정착 (`<Card><CardHeader/><CardContent/></Card>`).
2. **테넌트 인지 캐시 격리 ★★★★★** — `App.tsx` tenantId 변경 시 IndexedDB persister namespace 자동 갱신 (`createPersister({ tenantId })`). 멀티테넌시 원칙(원칙 4) 클라이언트까지 완벽 일관. 동급 B2B 제품에서도 드문 수준.
3. **TanStack Query advanced 활용** — WebSocket → polling fallback (`useScanProgress`), polling 조건부 (`useScans({ pollMs: 5000 })` active session만), persist + tenant key + maxAge 7일 + 보안 차단 list. 27회 useMutation 호출 모두 `onSuccess` invalidate 패턴 일관.
4. **PWA 결선** — offline indicator + update prompt + tenant 별 cache namespace + `/api` SW 우회(테넌트 유출 방지 D-PWA-7). v1 기준 매우 충실.
5. **mutation guard 패턴 (`mutationGuardTitle`)** — offline + permission 부족을 통일된 title tooltip으로 사용자에게 노출. 재사용 helper로 inconsistency 0.
6. **RBAC permission helper (`useHasPermission`)** — 서버 middleware와 mirror 정책으로 UI 선차단. fleet scope까지 정확히 평가.
7. **Theme switcher + i18n 동시 결선** — light/dark/system + ko/en 토글이 header에 1-click. enterprise admin tool 기본기.

---

## 4. 핵심 약점 (Critical Issues)

### P0 (사용자 매일 체감, 즉시 fix 필요)

**P0-1. Toast notification system 부재** ⚠️
- 현재: mutation 성공/실패가 **inline `<p className="text-sm text-destructive">`** 또는 `setSuccess('')` state 후 컴포넌트 위에 텍스트로만 표시. 일부 (robots.tsx `success` line 380 emerald-600) hand-coded.
- 영향: 사용자가 "내 행동이 통했나?" 즉각 확인 불가. window.confirm 후 결과 알림 없음. 페이지 이동 후 결과 사라짐.
- Linear/Vercel/Sentry는 모두 sonner-class toast 사용 — 사용자 신뢰의 핵심 시각 신호.

**P0-2. window.confirm / window.prompt 사용 (7건)** ⚠️
- 위치: `findings.tsx:168` (window.prompt dismiss reason), `integrations.tsx:221`, `sso.tsx:224,587`, `users.tsx:364` (window.confirm delete).
- 영향: 운영체제 기본 dialog로 **(1) 스타일링 불가 (2) i18n placeholder만 사용 가능 (3) brand 단절 (4) keyboard / screen reader 어색 (5) custom field (예: dismiss reason rich textarea) 불가능**.
- 자산 보유: `AlertDialog` (Radix) primitive 이미 import 가능한데 사용 안함.

**P0-3. Form validation 무표준** ⚠️
- 현재: 모든 form (robots create · scans start · invitations · webhooks · SSO · compliance profile) 이 **manual useState + onSubmit 시점 검증 + `<Input required>` HTML 검증** 혼용.
- 영향:
  - 에러 메시지 위치 불일치 (form 상단 vs 필드 아래 vs button title).
  - field-level realtime validation 없음 — 사용자 submit 후에야 알 수 있음.
  - HTML5 `required`만 의존 — 복잡 검증 (URL 형식, port range, JSON schema) 컴포넌트마다 hand-code.
  - submit button disabled state 일관성 없음 (`disabled={create.isPending || !canCreate || isOffline}` 만 — pristine/invalid 체크 없음).

**P0-4. Loading state — skeleton 0건** ⚠️
- 현재: 모든 isPending이 `<p className="text-sm text-muted-foreground">{t('common.loading')}</p>` 텍스트 한 줄. table은 `<TableRow><TableCell colSpan>{loading text}` 패턴 반복 (재사용 컴포넌트 없음).
- 영향: layout shift 발생 (loaded 후 row 채워지면서 페이지 전체 흔들림). enterprise 사용자가 "이거 죽었나?" 느낌. content placeholder가 perceived performance 50%+ 향상시킨다는 것이 web perf의 정설.
- 자산 보유: shadcn/ui `Skeleton` 컴포넌트 — **존재하지 않음** (스택에 미설치). 추가 필요.

**P0-5. Optimistic update 0건** ⚠️
- 27회 useMutation 검토 — `onMutate` / `setQueryData` / `useOptimistic` 사용 0건. invalidate-after-success 패턴만 사용.
- 영향:
  - delete robot → 서버 응답까지 ~300ms 동안 row가 그대로 보임. 사용자가 다시 클릭하는 사고 유발.
  - dismiss insight → list refresh 동안 계속 표시.
  - cancel scan → "cancelling..." 표시는 있지만 status badge가 즉시 변하지 않음.
- 본 약점은 React 19 `useOptimistic` 또는 TanStack Query `onMutate` rollback 패턴으로 30분/case 가능.

### P1 (주요 UX 부족, 다음 sprint)

**P1-1. Table 고급 기능 0** — sortable column 0건 (findings.tsx만 severity 정렬 로컬 함수 hand-code), column filter는 page-level state로 hand-rolled (3페이지 — robots/scans/findings 각각 다른 구현), bulk action 0 (selection checkbox 0), row action dropdown 0, pagination 없음 (limit hard-coded 10/20/50), column resize/hide 없음, virtual scroll 없음.
- robots list가 1000개 이상이면 화면 다운 (현 v1 spec엔 없을 수 있지만 enterprise 기대치).
- @tanstack/react-table 도입 + 공통 `<DataTable>` primitive 필요.

**P1-2. Mobile responsive 거의 0** — Tailwind responsive prefix (md:/sm:/lg:/xl:) 사용: layout 컴포넌트(`components/layout/`) 안에 **0건** (즉 sidebar/header가 mobile에서 그대로 노출되어 데스크톱 fixed width). 페이지 routes는 grid 일부 (`md:grid-cols-2`)만 — table 자체는 모바일에서 가로 스크롤 강제.
- B2B admin은 desktop 위주가 맞지만, **on-call 엔지니어가 phone에서 alert 보고 빠르게 확인**하는 use case 무시.
- Sidebar 모바일 hamburger menu 0, table → card 변환 0.

**P1-3. Inline editing 0** — 모든 edit은 별 form 또는 별 페이지. robot tags, criticality 같은 짧은 필드도 modal/form 강제 — Linear/Notion 류 "click cell to edit" 패턴 없음.

**P1-4. Drag-and-drop 0** — robot fleet 재할당 같은 자연스러운 DnD 기회 (현재는 form 입력) 없음. 다만 본 항목은 v2 검토 가능.

**P1-5. Command palette (Ctrl+K) 부재** — `cmdk` dep 이미 설치 + `command.tsx` shadcn primitive 존재. 그러나 **실 사용 0건** (route grep 0). 보안 감사 도구에서 "robot 빠르게 찾기", "audit log search", "특정 finding 점프"는 power user 표준 요구.

**P1-6. `Alert` 컴포넌트 deprecated 패턴** — `web/src/components/ui/alert.tsx`가 shadcn 표준이 아님 (직접 hand-coded, hard-coded `border-red-200 bg-red-50` slate/red Tailwind class). dark mode에서 깨질 가능성 (token 미사용). 그러나 검색 결과 실제 사용처 0 — 죽은 코드. 제거 또는 shadcn 표준으로 교체.

### P2 (polish, 여유 있을 때)

**P2-1. Re-render 최적화 0건** — useMemo / useCallback **총 9건 / 5 파일** (대부분 collapsed Set helper). zustand selector는 잘 분리되어 있어 OK이지만, scans.tsx의 RecentSessionsCard `list.filter()`는 매 5s polling마다 새 array — Filter 결과 memoize 안 됨. 영향 미미하지만 1000+ row scale 시 즉시 영향.

**P2-2. Bundle size 분석** — `internal/web/dist/assets/`:
  - main index: **340KB** (gzip ~110KB 추정 — large but ok)
  - utils: **149KB** (vendor 합산으로 보임)
  - select-CIwpvgP7.js: **51KB** (Radix Select 단독 — 모든 페이지에서 import — Tree shaking OK 확인됨)
  - 총 JS: **781KB raw** / ~250KB gzip 추정.
  - 평가: TanStack 풀스택 + Radix 19개 + cmdk + zustand로 이 정도는 industry baseline. 단 vite-bundle-visualizer 분석 후 vendor split + Radix lazy import 가능성 검토.

**P2-3. Polymorphic / composable API 일부 부족** — PageHeader `actions` slot은 좋은 패턴. 그러나 SummaryCard (overview.tsx) `ctaKey` 타입이 union string으로 제한 — `<Button>` children slot으로 받는 게 더 확장적. Similar pattern in BackupRow (system.tsx) — disabled button을 `<span>` for tooltip은 hack — `<Tooltip>` primitive 사용 권장.

**P2-4. inconsistent state — `success` 메시지 emerald-600 hard code** (robots.tsx:380) — token system 우회. theme 호환 깨짐 가능. (P0-1 toast로 일괄 해결)

**P2-5. `<dl>` semantic HTML 사용은 잘 됨** (packs.$packKey.tsx Meta 컴포넌트). 그러나 다른 페이지의 메타 표시는 `<div className="grid grid-cols-1 sm:grid-cols-[12rem_1fr]">` Row helper — 같은 의미인데 다른 코드. **공통 `<DescriptionList>` primitive 추출 권장**.

---

## 5. 권장 개선안

### 5.1 Component patterns

- **`<DataTable>` primitive** (shadcn/ui examples 참고) — sortable / filterable / paginated / column selection / virtualized 단일 컴포넌트. 모든 list 페이지(robots/scans/findings/reports/integrations/users/sso) 통합.
- **`<DescriptionList>`** — packs.$packKey.tsx Meta, robots.$robotId.tsx MetaRow, system.tsx Row 3패턴 통합.
- **`<ConfirmDialog>`** — AlertDialog wrapper. destructive action (delete robot/cancel scan/dismiss insight/delete webhook/delete SSO/revoke invitation) 일괄 적용.
- **`<FormField>`** — react-hook-form + zod resolver 결선 + Label + Input + error message 표준 wrapping. shadcn/ui 공식 `Form` primitive 추가 (별 npx install — `pnpm dlx shadcn@latest add form`).
- **`<EmptyState action>` polymorphic** — 이미 잘 설계됨. 유지.

### 5.2 Interaction design

- **Toast (sonner)** — `<Toaster position="bottom-right" richColors closeButton />` 를 `_authenticated/route.tsx`에 1회 추가. 모든 mutation `onSuccess` / `onError`에 `toast.success(t(...))` / `toast.error(...)` 1줄로 표준화. promise toast (`toast.promise(mutation, { loading, success, error })`)는 long-running mutation (scan start 등)에 즉시 적용.
- **Skeleton loading** — `<Skeleton className="h-12 w-full" />` n행 — table loading state 일관. `pnpm dlx shadcn@latest add skeleton`.
- **Optimistic update** — useMutation `onMutate` + rollback pattern. delete/dismiss 류 5건 우선 적용. React 19 `useOptimistic`은 form submission 류에 보조 (TanStack Query 가 server cache main).
- **Confirmation dialog** — `AlertDialog` 표준화. window.confirm 7건 일괄 교체.

### 5.3 Form patterns

```tsx
// 권장 표준 패턴 (react-hook-form + zod)
const robotSchema = z.object({
  fleetId: z.string().min(1, t('robots.form.fleet.required')),
  name: z.string().min(1).max(128),
  host: z.string().min(1),
  port: z.number().int().min(1).max(65535),
  authType: z.enum(['password', 'privateKey']),
  // ... discriminated union for password vs privateKey
})
const form = useForm<z.infer<typeof robotSchema>>({
  resolver: zodResolver(robotSchema),
  defaultValues: { port: 22, authType: 'password', criticality: 'medium' },
})
```
- field-level realtime validation + submit button `disabled={!form.formState.isValid || form.formState.isSubmitting}`.
- error 메시지 위치: 필드 바로 아래 + `<FormMessage>` 표준 컴포넌트.

### 5.4 Table interactions

- @tanstack/react-table 도입 + shadcn `data-table` example 패턴.
- 우선 적용: robots / scans / findings / reports.
- sortable column header (severity desc 등 client sort), column-level filter (placeholder + global filter input), row selection (checkbox + bulk action toolbar), pagination (server cursor OR client slice), column hide/resize via DropdownMenu in header.

### 5.5 Responsiveness

- Sidebar mobile: viewport `md` 이하에서 hamburger button + Sheet (Radix Dialog with side) drawer. Header에 `<Menu>` 아이콘 (mobile only).
- Table: `md` 이하에서 row → Card 변환 (별 `<DataTableMobileCard>` slot). Or 단순히 `overflow-x-auto` + sticky first column.
- Touch target 44x44 최소 — 현재 Button h-8/h-9/h-10 — 모바일 page에선 size="default" (h-10) 강제 검토.

### 5.6 Performance

- `vite-bundle-visualizer` (dev) — main 340KB 안에 뭐가 큰지 확인. 예상: TanStack Router + Query + 19개 Radix primitive 의 vendor 분리 누락.
- Vite config `build.rollupOptions.output.manualChunks` 로 vendor 분리:
  ```ts
  manualChunks: {
    'react-vendor': ['react', 'react-dom'],
    'tanstack': ['@tanstack/react-router', '@tanstack/react-query', ...],
    'radix': [/* @radix-ui/* */],
  }
  ```
- Route-level code split는 이미 `autoCodeSplitting: true`로 작동 중 (확인 — 각 route별 chunk 있음). ✅
- useMemo는 scans.tsx RecentSessionsCard filter, system.tsx ScansSeverityCard totals 계산 등에 부분 적용 가능 (현재는 매 render 매번 계산). 측정 후 적용 권장.

### 5.7 State management

- 현재 경계 명확 ★★★★ — Server (TanStack Query), Client (Zustand auth/theme/locale), Form (local useState).
- 권장: Form state는 react-hook-form으로 migrate (P0-3 fix와 동시). URL search state는 이미 TanStack Router validateSearch 사용 (scans.tsx `?session=`) 잘 됨.
- localStorage persist 4건 (auth, theme, locale, robot results collapsed sessions). 일관성 OK — 단 collapsed sessions만 raw localStorage hand-code (다른 3건은 zustand persist middleware). 통일 (4번째도 zustand store로) 권장.

### 5.8 Component API design

- props 명명: 일관성 ★★★★ (action / actions / onSelect / onDismiss / onCreated). 다만 일부 onSubmit 콜백이 inline arrow function — useCallback memo 없이도 OK이지만 `<Button onClick={() => mutation.mutate(...)}>` 패턴은 재생성 — Button forwardRef + memo 효과 미미. 영향 0 — 무시 가능.

---

## 6. 즉시 적용 가능 P0 fix

각 1~3시간 내 commit 가능. 큰 design doc 불필요 — 패턴 1회 정립 후 페이지마다 grep & replace.

### Fix-1: sonner Toast 도입 (~2h)
- `pnpm add sonner` — 단일 dep.
- `web/src/routes/_authenticated/route.tsx`에 `<Toaster position="bottom-right" richColors closeButton />` 추가.
- mutation `onSuccess` → `toast.success(t(...))` 변경. `onError` → `toast.error(...)` 변경.
- 영향 페이지: robots / scans / findings / users / integrations / sso / compliance / advisor (~12 mutation site).

### Fix-2: AlertDialog로 window.confirm/prompt 교체 (~3h)
- `web/src/components/ui/confirm-dialog.tsx` 신규 — AlertDialog wrapper. props: `title, description, confirmLabel, cancelLabel, variant ('default' | 'destructive'), onConfirm`.
- 7건 사용처 일괄 교체.
- `findings.tsx`의 dismiss reason은 별 `<DismissInsightDialog>` (Dialog + textarea + reason validation) 신규.

### Fix-3: react-hook-form + zod 도입 (1차 form 1개만, ~3h)
- `pnpm add react-hook-form zod @hookform/resolvers`
- `pnpm dlx shadcn@latest add form` (shadcn Form primitive).
- 최초 적용: CreateRobotForm (robots.tsx) — 가장 큰 폼이라 ROI 최대 + 패턴 검증.
- 검증 통과 후 다른 form 일괄 마이그레이션 (별 sprint).

### Fix-4: Skeleton loading state (~1h)
- `pnpm dlx shadcn@latest add skeleton`.
- 공통 `<TableSkeleton rows={5} cols={5} />` 컴포넌트 신규.
- table 로딩 상태 일괄 교체.

### Fix-5: useOptimistic delete/dismiss (~2h)
- robots.$robotId.tsx DeleteRobotCard, findings.tsx dismiss, integrations.tsx delete endpoint 3건 우선.
- useMutation `onMutate: () => { previous = queryClient.getQueryData; queryClient.setQueryData(prev => prev.filter(...)); return { previous } }, onError: (_, __, ctx) => queryClient.setQueryData(ctx.previous)`.

**합계**: ~11h. 1 dev sprint (1.5일) 가능. UX 체감 가장 큰 P0 5건 해결.

---

## 7. 별 round design doc 후보

큰 작업은 design doc 우선 (CLAUDE.md memory 참조 — feedback_design_doc_first).

| # | 주제 | 우선순위 | 예상 | 산출 |
|---|---|---|---|---|
| D-UI-1 | **Toast notification system** | P0 | ~2h (design 없이 가능) | sonner + 표준 helper |
| D-UI-2 | **Form validation 표준화** (react-hook-form + zod + shadcn Form) | P1 | 2~3일 (design doc 권장) | 모든 form 마이그레이션 plan + validation schema convention |
| D-UI-3 | **DataTable primitive** (@tanstack/react-table 통합) | P1 | 1주 (design doc 필수) | sortable/filterable/paginated/virtualized 단일 컴포넌트 + 모든 list 페이지 migration |
| D-UI-4 | **Mobile responsive** (sidebar drawer + table mobile card) | P1 | 1~2주 (design doc 필수) | breakpoint 가이드 + Sheet sidebar + DataTable mobile mode |
| D-UI-5 | **Command palette (Ctrl+K)** | P2 | 2~3주 (design doc 필수) | cmdk 결선 + 라우트 검색 + recent + global action |
| D-UI-6 | **Skeleton loading + perceived performance** | P0 | ~1h | shadcn Skeleton + TableSkeleton/CardSkeleton helper |
| D-UI-7 | **Optimistic update 표준 패턴** | P0 | ~3h (design 없이 가능) | onMutate rollback helper + 5건 적용 |
| D-UI-8 | **bundle 분석 + vendor chunk split** | P2 | ~1일 (design doc 권장) | vite-bundle-visualizer 결과 + manualChunks 설정 |

권장 sprint 순서:
1. **D-UI-1 + D-UI-6 + D-UI-7** (P0 toast + skeleton + optimistic) — half day, ROI 최대.
2. **D-UI-2** (form 표준화) — 1주.
3. **D-UI-3** (DataTable) — 1주.
4. **D-UI-4** (mobile) — 1~2주.
5. **D-UI-5 + D-UI-8** (polish) — 여유 시.

---

## 8. 권장 dep 추가 정리

| dep | 버전 | 크기 (gzip) | 우선순위 | 비고 |
|---|---|---|---|---|
| sonner | ^1.7.x | ~5KB | **P0** | Toast — vercel 제작, shadcn 권장 |
| react-hook-form | ^7.54.x | ~10KB | **P0** | Form state + validation |
| zod | ^3.24.x | ~14KB | **P0** | Schema validation + TS infer |
| @hookform/resolvers | ^3.10.x | ~1KB | **P0** | react-hook-form ↔ zod 어댑터 |
| @tanstack/react-table | ^8.20.x | ~14KB | P1 | Headless table — TanStack 생태 일관 |
| vaul (선택) | ^1.1.x | ~5KB | P1 | Mobile drawer (vs Radix Dialog) |
| vite-bundle-visualizer | dev only | - | P2 | dev 분석 |

**제거 검토**: `web/src/components/ui/alert.tsx` (shadcn 표준 아님, 사용처 0) — 또는 shadcn 표준으로 재생성 (`pnpm dlx shadcn@latest add alert`).

---

## 9. 다른 페르소나 cross-reference 필요 항목

- **Visual 페르소나**: P0-1 toast 디자인 톤 (success green vs default vs destructive), skeleton shimmer 색 (`bg-muted` vs gradient), severity stats card 색상 강도(현재 critical/high 둘 다 destructive 톤 동일 — visual hierarchy 약함).
- **UX research 페르소나**: P0-3 form validation 메시지 톤 (감사 도구 → 정확/엄격 vs 친근/안내), confirmation dialog wording (delete robot은 "This will permanently delete..." vs "삭제하시겠습니까?"), command palette grouping 우선순위.
- **Accessibility 페르소나**: window.confirm 교체 시 AlertDialog의 focus trap / Esc 처리 / screen reader announcement 검증, Skeleton의 aria-busy / aria-live, toast의 aria-live="polite" vs assertive 결정, mobile sidebar drawer focus 관리.

---

## 부록 A — 코드베이스 정량 요약

- **컴포넌트**: ui primitive 23 + layout 5 + PWA 2 = 30
- **라우트**: 26 (authenticated 23 + public 3)
- **API hook 파일**: 1 (2310 lines — 큰 편, 도메인별 분할 고려 가능)
- **stores**: 2 (auth + theme; locale은 i18n/store.ts에 별도)
- **window.confirm/prompt**: 7건 (4 페이지)
- **useMutation 호출 site**: 27건
- **useMemo/useCallback**: 9건 / 5 파일 (적은 편)
- **mobile breakpoint (md:/sm:) layout 영역**: 0건
- **Skeleton 사용**: 0건
- **Toast 사용**: 0건
- **react-hook-form / zod 사용**: 0건
- **bundle JS total**: 781KB raw / ~250KB gzip 추정
- **largest chunk**: main index 340KB, utils 149KB, select 51KB

## 부록 B — 주요 파일 경로

| 영역 | 경로 |
|---|---|
| Vite config | `web/vite.config.ts` |
| App entry | `web/src/App.tsx`, `web/src/main.tsx` |
| Authenticated layout | `web/src/routes/_authenticated/route.tsx` |
| Sidebar | `web/src/components/layout/Sidebar.tsx` |
| Header | `web/src/components/layout/Header.tsx` |
| PageHeader / Breadcrumbs / EmptyState | `web/src/components/layout/*.tsx` |
| UI primitives | `web/src/components/ui/*.tsx` (23 파일) |
| API hooks | `web/src/api/hooks.ts` (2310 줄) |
| Auth store | `web/src/stores/auth.ts` |
| Theme store | `web/src/stores/theme.ts` |
| Main offender (window.confirm) | `web/src/routes/_authenticated/{findings,users,integrations,sso}.tsx` |
| Form 후보 (rhf migration 1st) | `web/src/routes/_authenticated/robots.tsx` CreateRobotForm |
| Bundle output | `internal/web/dist/assets/` |
