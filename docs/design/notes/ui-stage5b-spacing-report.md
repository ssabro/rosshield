# UI Stage 5b — Drill-down + 일반 페이지 spacing 일관화

> v0.6.4 직후 후속 polish. Stage 5(`ui-stage5-polish-report.md`)에서 axe scan 5
> 페이지(overview/findings/scans/robots/fleets)는 `space-y-4`로 일관화 완료.
> 본 round는 **drill-down 페이지 + 잔여 일반 페이지**를 cover합니다.

## 1. 분포 진단

`web/src/routes/_authenticated/` 19 페이지의 root `<div>` spacing 분포:

| 페이지 | 카테고리 | Root spacing (before) | 결정 |
|---|---|---|---|
| advisor.tsx | 일반 | `space-y-4` | 유지 |
| audit.tsx | 일반 | `space-y-4` | 유지 |
| **compliance.tsx** | 일반 (PageHeader + form + 1 section + snapshots) | **`space-y-6`** | → `space-y-4` |
| findings.tsx | 일반 (axe scan) | `space-y-4` | 유지 (Stage 5 적용 완료) |
| fleets.tsx | 일반 (axe scan) | `space-y-4` | 유지 (Stage 5 적용 완료) |
| **fleets.$fleetId.tsx** | drill-down (3 섹션) | **`space-y-6`** × 3 | → `space-y-4` × 3 |
| integrations.tsx | 일반 (제외 영역) | `space-y-4` | 유지 |
| license.tsx | 일반 | `space-y-4` | 유지 |
| overview.tsx | 일반 (axe scan) | `space-y-4` | 유지 (Stage 5 적용 완료) |
| packs.$packKey.checks.$checkId.tsx | drill-down (2단 nested) | `space-y-4` | 유지 |
| packs.$packKey.tsx | drill-down | `space-y-4` | 유지 |
| reports.tsx | 일반 | `space-y-4` | 유지 |
| **robots.$robotId.tsx** | drill-down (5+ 카드, ~1100줄) | **`space-y-6`** × 3 | **carryover** (유지) |
| robots.tsx | 일반 (axe scan) | `space-y-4` | 유지 (Stage 5 적용 완료) |
| scans.tsx | 일반 (axe scan) | `space-y-4` | 유지 (Stage 5 적용 완료) |
| settings.tsx | 일반 | `space-y-4` | 유지 |
| sso.tsx | 일반 | `space-y-4` | 유지 |
| system.tsx | 일반 | `space-y-4` | 유지 |
| users.tsx | 일반 | `space-y-4` | 유지 |

**Before 분포**: 19 페이지 중 `space-y-4` × 16, `space-y-6` × 3.
**After 분포**: 19 페이지 중 `space-y-4` × 18, `space-y-6` × 1.

## 2. 표준값 결정

**페이지 root 표준: `space-y-4`** — Stage 5 axe scan 5 페이지에서 채택한 값을
전체 페이지로 확장. 다수 빈도 일관성 우선.

**예외 — `space-y-6` 유지 조건**:
- 5+ 카드/섹션의 큰 detail 페이지
- 시각적으로 카드 간 분리가 강조될 필요 있음 (예: credential rotation +
  히스토리 + delete 등 위험 액션 카드 혼재)
- 본 round에서는 `robots.$robotId.tsx` 1건만 해당.

**결정 근거**:
- Stage 5에서 `space-y-4`가 다수(20 페이지 중 14+)였고 axe scan 5 페이지에서
  채택된 사실상 표준. Stage 5b는 잔여 일반 페이지를 같은 값으로 통일.
- `fleets.$fleetId.tsx`는 3 섹션 (meta Card + robots Card + back link)으로
  작은 분량 → `space-y-4`로 통일 OK.
- `compliance.tsx`는 4 섹션 (header + form + table section + snapshots)이지만
  각 섹션 자체가 자체 spacing(`space-y-2`)을 가지므로 root는 `space-y-4`로
  충분.
- `robots.$robotId.tsx`는 PageHeader + meta Card + RobotResultsCard +
  RotateCredentialCard + DeleteRobotCard로 5 카드. credential rotation /
  delete 등 위험 액션 분리 강조 필요 → carryover.

## 3. 적용 — Before vs After

### 3.1 compliance.tsx (1 변경)

```diff
- <div className="space-y-6">
+ // D-UI-1 Stage 5b — 페이지 root 표준 `space-y-4` (drill-down/일반 일관화).
+ <div className="space-y-4">
```
line 90 main render path.

### 3.2 fleets.$fleetId.tsx (3 변경)

```diff
  if (fleetQuery.isPending) {
    return (
-     <div className="space-y-6">
+     // D-UI-1 Stage 5b — 페이지 root 표준 `space-y-4` (drill-down 일관화).
+     <div className="space-y-4">
        ...
    )
  }
  if (!fleet || fleetQuery.isError) {
    return (
-     <div className="space-y-6">
+     <div className="space-y-4">
        ...
    )
  }
  return (
-   <div className="space-y-6">
+   <div className="space-y-4">
```
line 37 (loading), line 46 (error), line 61 (main) — 3 진입 경로 동일하게 통일.

## 4. 카드 padding (옵션) — 진단만, 변경 0

`p-[2468]` grep 결과:
- `advisor.tsx:201` — `flex flex-col gap-3 rounded-md border bg-card p-4`: Card
  컴포넌트가 아닌 일반 `<section>`, 의도된 패딩.
- `sso.tsx:331` — `space-y-3 p-4`: dialog 내부 일반 div, 의도된 패딩.

`web/src/components/ui/card.tsx`는 shadcn 기본 (CardHeader `p-6`, CardContent
`p-6 pt-0`) 유지. 페이지에서 카드 padding override는 0건.

**결론**: 카드 padding 변경 불필요. 현재 정상.

## 5. 미적용 (carryover)

| 페이지 | 사유 | 후속 round |
|---|---|---|
| robots.$robotId.tsx | 5+ 카드 (meta + 결과 이력 + credential rotate + delete), 위험 액션 분리 강조 필요. visual hierarchy 측면에서 `space-y-6` 적절. | Stage 5c 이후 별도 평가 (전체 detail 페이지 hierarchy 표준화 시) |

## 6. 검증

- `pnpm exec tsc --noEmit` PASS (변경 후, 0 errors)
- `pnpm exec vitest run` 회귀 0 — 실패 2건은 pre-existing:
  - `src/lib/manifest.test.ts`: "rosshield Console" 기대 vs "Lodestar 관리자
    콘솔" 실제. v0.6.4 브랜드 전환 carryover, 본 task 무관.
  - PWA virtual module import 실패: 환경 issue, spacing 변경 무관.
  - stash 후 동일 실패 재현 확인.
- `pnpm build` PASS (~15초, dist 정상 생성).

## 7. 시각적 영향

- screenshot 0 (사용자 자체 검증).
- `space-y-6` (24px) → `space-y-4` (16px)로 카드 간 간격 8px 감소.
- compliance / fleets detail 2 페이지에서 visual density 소폭 상승. 다른 17
  페이지와 일관된 hierarchy 제공.
- robots.$robotId.tsx는 의도적으로 `space-y-6` 유지하여 5 카드 분리 강조.
