import { createFileRoute } from '@tanstack/react-router'

import { Inbox, ShieldAlert, ShieldCheck } from 'lucide-react'

import { ApiError } from '@/api/errors'
import { useComplianceEffectiveness } from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useT } from '@/i18n/t'
import { requirePermission } from '@/lib/route-guards'

import type {
  ComplianceCategory,
  ComplianceEffectivenessResponse,
} from '@/api/hooks'
import type { DictKey } from '@/i18n/dict'

// `/compliance/effectiveness` — Phase 11.B-6 SOC2 통제 effectiveness dashboard.
//
// design doc: docs/design/notes/soc2-readiness-design.md §7.6.
//
// 본 페이지는 외부 감사인 위임 표면 — admin + auditor (audit.export 권한, server gate).
// backend: GET /api/v1/compliance/effectiveness (handlers/compliance_effectiveness.go).
// hook: useComplianceEffectiveness — 5 분 polling.
//
// 산출:
//   - 큰 cover% 숫자 + Progress + Badge (healthy/warning/critical)
//   - 14 카테고리 매트릭스 표 (CC1~CC9 + A1·A2·A5) — cover% · audit 7d/30d · gaps
//   - external-track gaps 카드 강조
//
// /regions 페이지 컴포넌트 구조 cargo cult — Skeleton/Empty/Error 분기 동일 패턴.

export function ComplianceEffectivenessPage(): React.ReactElement {
  const t = useT()
  const q = useComplianceEffectiveness()

  const headerBadge = q.isSuccess ? (
    <Badge variant={coverVariant(q.data.coverPercent)} className="text-[10px]">
      {formatPercent(q.data.coverPercent)} ·{' '}
      {t(coverThresholdKey(q.data.coverPercent))}
    </Badge>
  ) : undefined

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.compliance.effectiveness.title')}
        description={t('pages.compliance.effectiveness.description')}
        badge={headerBadge}
      />

      {q.isPending && <EffectivenessSkeleton />}
      {q.isError && <EffectivenessError error={q.error} />}
      {q.isSuccess && q.data.totalSubControls === 0 && (
        <EmptyState
          icon={Inbox}
          title={t('compliance.dashboard.empty.title')}
          description={t('compliance.dashboard.empty.description')}
          className="border-dashed"
        />
      )}
      {q.isSuccess && q.data.totalSubControls > 0 && (
        <>
          <CoverHeroCard data={q.data} />
          <CategoryMatrix categories={q.data.categories} />
          <GapsCard categories={q.data.categories} />
        </>
      )}
    </div>
  )
}

function CoverHeroCard({
  data,
}: {
  data: ComplianceEffectivenessResponse
}): React.ReactElement {
  const t = useT()
  const percent = data.coverPercent
  const variant = coverVariant(percent)

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('compliance.dashboard.cover.label')}
        </CardTitle>
        <CardDescription className="text-xs">
          {t('compliance.dashboard.cover.subControls', {
            covered: data.coveredSubControls,
            total: data.totalSubControls,
          })}
          {data.generatedAt && (
            <>
              {' · '}
              {t('compliance.dashboard.cover.updatedAt', {
                at: formatDate(data.generatedAt),
              })}
            </>
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex items-baseline gap-3">
          <span className="text-4xl font-semibold tracking-tight">
            {formatPercent(percent)}
          </span>
          <Badge variant={variant}>{t(coverThresholdKey(percent))}</Badge>
        </div>
        <Progress value={percent} className="h-2" />
      </CardContent>
    </Card>
  )
}

function CategoryMatrix({
  categories,
}: {
  categories: ComplianceCategory[]
}): React.ReactElement {
  const t = useT()
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          SOC2 Categories
        </CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('compliance.dashboard.table.code')}</TableHead>
                <TableHead>{t('compliance.dashboard.table.name')}</TableHead>
                <TableHead className="text-right">
                  {t('compliance.dashboard.table.subControls')}
                </TableHead>
                <TableHead className="text-right">
                  {t('compliance.dashboard.table.covered')}
                </TableHead>
                <TableHead className="text-right">
                  {t('compliance.dashboard.table.coverPercent')}
                </TableHead>
                <TableHead className="text-right">
                  {t('compliance.dashboard.table.audit7d')}
                </TableHead>
                <TableHead className="text-right">
                  {t('compliance.dashboard.table.audit30d')}
                </TableHead>
                <TableHead className="text-right">
                  {t('compliance.dashboard.table.gaps')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {categories.map((cat) => (
                <CategoryRow key={cat.code} category={cat} />
              ))}
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  )
}

function CategoryRow({
  category,
}: {
  category: ComplianceCategory
}): React.ReactElement {
  const t = useT()
  const variant = coverVariant(category.coverPercent)
  const nameKey = categoryNameKey(category.code)
  const name = nameKey ? t(nameKey) : category.name
  return (
    <TableRow>
      <TableCell className="font-mono text-xs">{category.code}</TableCell>
      <TableCell className="text-sm">{name}</TableCell>
      <TableCell className="text-right font-mono text-xs">{category.subControls}</TableCell>
      <TableCell className="text-right font-mono text-xs">{category.covered}</TableCell>
      <TableCell className="text-right">
        <Badge variant={variant} className="text-[10px]">
          {formatPercent(category.coverPercent)}
        </Badge>
      </TableCell>
      <TableCell className="text-right font-mono text-xs text-muted-foreground">
        {category.auditEvents.last7Days}
      </TableCell>
      <TableCell className="text-right font-mono text-xs text-muted-foreground">
        {category.auditEvents.last30Days}
      </TableCell>
      <TableCell className="text-right font-mono text-xs text-muted-foreground">
        {category.gaps.length}
      </TableCell>
    </TableRow>
  )
}

function GapsCard({
  categories,
}: {
  categories: ComplianceCategory[]
}): React.ReactElement {
  const t = useT()
  const allGaps: Array<{ code: string; gap: string }> = []
  for (const cat of categories) {
    for (const g of cat.gaps) {
      allGaps.push({ code: cat.code, gap: g })
    }
  }
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm font-medium">
          <ShieldAlert className="size-4 text-amber-500" aria-hidden />
          {t('compliance.dashboard.gaps.title')}
        </CardTitle>
        <CardDescription className="text-xs">
          {t('compliance.dashboard.gaps.description')}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {allGaps.length === 0 ? (
          <p className="flex items-center gap-2 text-xs text-muted-foreground">
            <ShieldCheck className="size-4 text-emerald-500" aria-hidden />
            {t('compliance.dashboard.gaps.none')}
          </p>
        ) : (
          <ul className="divide-y divide-border rounded-md border">
            {allGaps.map((g, idx) => (
              <li
                key={`${g.code}-${idx}`}
                className="flex items-center gap-3 px-3 py-2 text-xs"
              >
                <Badge variant="outline" className="shrink-0 font-mono text-[10px]">
                  {g.code}
                </Badge>
                <span className="text-foreground">{g.gap}</span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

function EffectivenessSkeleton(): React.ReactElement {
  return (
    <div className="space-y-4" role="status" aria-label="불러오는 중">
      <Card>
        <CardContent className="space-y-3 p-6">
          <Skeleton className="h-5 w-1/3" />
          <Skeleton className="h-10 w-1/2" />
          <Skeleton className="h-3 w-full" />
        </CardContent>
      </Card>
      <Card>
        <CardContent className="space-y-3 p-6">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-4 w-full" />
          ))}
        </CardContent>
      </Card>
    </div>
  )
}

function EffectivenessError({
  error,
}: {
  error: unknown
}): React.ReactElement {
  const t = useT()
  let descKey: DictKey = 'compliance.dashboard.error.fallback'
  if (error instanceof ApiError) {
    if (error.status === 401 || error.status === 403) {
      descKey = 'compliance.dashboard.error.unauthorized'
    } else if (error.status === 503) {
      descKey = 'compliance.dashboard.error.unavailable'
    }
  }
  return (
    <EmptyState
      icon={ShieldAlert}
      title={t('compliance.dashboard.error.title')}
      description={t(descKey)}
      className="border-dashed border-destructive/40"
    />
  )
}

// coverVariant — cover% 임계값을 shadcn Badge variant 로 매핑합니다.
//   ≥ 90 → default (healthy)
//   70 ~ 89 → secondary (warning)
//   < 70 → destructive (critical)
// 단위 테스트 대상 (compliance.effectiveness.test.tsx).
export function coverVariant(
  percent: number,
): 'default' | 'secondary' | 'destructive' {
  if (percent >= 90) return 'default'
  if (percent >= 70) return 'secondary'
  return 'destructive'
}

// coverThresholdKey — cover% 를 i18n threshold 키로 매핑합니다.
// 단위 테스트 대상.
export function coverThresholdKey(
  percent: number,
):
  | 'compliance.dashboard.threshold.healthy'
  | 'compliance.dashboard.threshold.warning'
  | 'compliance.dashboard.threshold.critical' {
  if (percent >= 90) return 'compliance.dashboard.threshold.healthy'
  if (percent >= 70) return 'compliance.dashboard.threshold.warning'
  return 'compliance.dashboard.threshold.critical'
}

// formatPercent — 0~100 float 을 "82.5%" 등 1 자리 소수로 표시합니다.
// 단위 테스트 대상.
export function formatPercent(percent: number): string {
  return `${percent.toFixed(1)}%`
}

// formatDate — RFC3339 문자열을 사용자 가시 timestamp 로 변환.
function formatDate(isoLike: string): string {
  try {
    return new Date(isoLike).toLocaleString()
  } catch {
    return isoLike
  }
}

// categoryNameKey — category code 별 i18n 키 dispatch. unknown code 면 undefined
// 반환 → 호출자가 backend 원문(category.name) 으로 fallback.
// 단위 테스트 대상.
export function categoryNameKey(code: string): DictKey | undefined {
  switch (code) {
    case 'CC1':
      return 'compliance.dashboard.category.CC1'
    case 'CC2':
      return 'compliance.dashboard.category.CC2'
    case 'CC3':
      return 'compliance.dashboard.category.CC3'
    case 'CC4':
      return 'compliance.dashboard.category.CC4'
    case 'CC5':
      return 'compliance.dashboard.category.CC5'
    case 'CC6':
      return 'compliance.dashboard.category.CC6'
    case 'CC7':
      return 'compliance.dashboard.category.CC7'
    case 'CC8':
      return 'compliance.dashboard.category.CC8'
    case 'CC9':
      return 'compliance.dashboard.category.CC9'
    case 'A1':
      return 'compliance.dashboard.category.A1'
    case 'A2':
      return 'compliance.dashboard.category.A2'
    case 'A5':
      return 'compliance.dashboard.category.A5'
    default:
      return undefined
  }
}

export const Route = createFileRoute('/_authenticated/compliance/effectiveness')(
  {
    beforeLoad: () => requirePermission('audit', 'export'),
    component: ComplianceEffectivenessPage,
  },
)
