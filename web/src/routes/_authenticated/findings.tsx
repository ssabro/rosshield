import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { ApiError } from '@/api/errors'
import { useDismissInsight, useHasPermission, useInsights } from '@/api/hooks'
import { SeverityBadge } from '@/components/common/SeverityBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { confirm } from '@/lib/confirm'
import { undoableAction } from '@/lib/undoable'
import { mutationGuardTitle, useIsOffline } from '@/lib/use-is-offline'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { TableRowSkeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type { Insight } from '@/api/hooks'
import type { Severity } from '@/lib/severity'

// `/findings` — Insight 목록 (E19-1).
// - kind/severity/robotId 필터 (모두 선택 옵션)
// - 각 row의 "Dismiss" 버튼 → ConfirmDialog → API 호출 → 자동 invalidate
// - D-UI-1 Stage 4 공통 패턴 (SeverityBadge · EmptyState · Skeleton · Toast · ConfirmDialog).
const KIND_OPTIONS = ['drift', 'anomaly', 'peer', 'root_cause', 'prediction'] as const
const SEVERITY_OPTIONS = ['info', 'low', 'medium', 'high', 'critical'] as const

function FindingsPage(): React.ReactElement {
  const [kind, setKind] = useState<string>('')
  const [severity, setSeverity] = useState<string>('')
  const [robotId, setRobotId] = useState<string>('')
  const t = useT()

  const filter = buildInsightsFilter({ kind, severity, robotId })
  const insights = useInsights(filter)
  const hasActiveFilter = kind !== '' || severity !== '' || robotId.trim() !== ''
  const resetFilters = (): void => {
    setKind('')
    setSeverity('')
    setRobotId('')
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.findings.title')}
        description={t('pages.findings.description')}
      />

      {insights.isSuccess && insights.data.length > 0 && (
        <SeverityStats insights={insights.data} onClick={(s) => setSeverity(s)} active={severity} />
      )}

      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <div className="flex flex-col gap-2">
          <Label htmlFor="kind-filter">{t('findings.filter.kind')}</Label>
          <Select value={kind || 'all'} onValueChange={(v) => setKind(v === 'all' ? '' : v)}>
            <SelectTrigger id="kind-filter">
              <SelectValue placeholder={t('findings.filter.all')} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t('findings.filter.all')}</SelectItem>
              {KIND_OPTIONS.map((k) => (
                <SelectItem key={k} value={k}>
                  {k}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="severity-filter">{t('findings.filter.severity')}</Label>
          <Select value={severity || 'all'} onValueChange={(v) => setSeverity(v === 'all' ? '' : v)}>
            <SelectTrigger id="severity-filter">
              <SelectValue placeholder={t('findings.filter.all')} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t('findings.filter.all')}</SelectItem>
              {SEVERITY_OPTIONS.map((s) => (
                <SelectItem key={s} value={s}>
                  {s}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="robot-filter">{t('findings.filter.robot')}</Label>
          <Input
            id="robot-filter"
            placeholder={t('findings.filter.robot.placeholder')}
            value={robotId}
            onChange={(e) => setRobotId(e.target.value)}
          />
        </div>
      </div>

      {/* 모바일 반응형: 표는 overflow-x-auto로 가로 스크롤 허용 — md 이하 시 액션 셀까지 노출. */}
      <div className="rounded-md border">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('findings.table.kind')}</TableHead>
                <TableHead>{t('findings.table.severity')}</TableHead>
                <TableHead>{t('findings.table.summary')}</TableHead>
                <TableHead>{t('findings.table.scope')}</TableHead>
                <TableHead>{t('findings.table.created')}</TableHead>
                <TableHead className="text-right">{t('findings.table.action')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {insights.isPending && (
                <TableRow>
                  <TableCell colSpan={6} className="p-3">
                    <TableRowSkeleton rows={5} columns={6} />
                  </TableCell>
                </TableRow>
              )}
              {insights.isError && (
                <TableRow>
                  <TableCell colSpan={6} className="p-0">
                    <EmptyState
                      variant="loading-fail"
                      title={
                        insights.error instanceof ApiError
                          ? insights.error.message
                          : t('findings.error.fallback')
                      }
                      description={t('findings.error.fallback')}
                      className="rounded-none border-0 bg-transparent"
                    />
                  </TableCell>
                </TableRow>
              )}
              {insights.isSuccess && insights.data.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="p-0">
                    <EmptyState
                      variant={hasActiveFilter ? 'search-no-result' : 'no-data'}
                      title={t(
                        hasActiveFilter
                          ? 'findings.empty.filtered.title'
                          : 'findings.empty.noFilter.title',
                      )}
                      description={t(
                        hasActiveFilter
                          ? 'findings.empty.filtered.description'
                          : 'findings.empty.noFilter.description',
                      )}
                      action={
                        hasActiveFilter ? (
                          <Button size="sm" variant="outline" onClick={resetFilters}>
                            {t('findings.empty.filtered.cta')}
                          </Button>
                        ) : undefined
                      }
                      className="rounded-none border-0 bg-transparent"
                    />
                  </TableCell>
                </TableRow>
              )}
              {insights.isSuccess &&
                sortInsightsBySeverityIfUnfiltered(insights.data, severity).map((ins) => (
                  <InsightRow key={ins.id} insight={ins} />
                ))}
            </TableBody>
          </Table>
        </div>
      </div>
    </div>
  )
}

// SEVERITY_KEYS — SeverityBadge가 받는 5종 Severity. 알 수 없는 값은 'info'로 fallback.
const SEVERITY_KEYS: ReadonlySet<Severity> = new Set([
  'critical',
  'high',
  'medium',
  'low',
  'info',
])

function toSeverityKey(value: string): Severity {
  return SEVERITY_KEYS.has(value as Severity) ? (value as Severity) : 'info'
}

function InsightRow({ insight }: { insight: Insight }): React.ReactElement {
  const dismiss = useDismissInsight()
  const t = useT()
  // RBAC Stage 5 — insight dismiss는 fleet[X].insight.write (§2.2 ID 12).
  //   insight.fleetId가 있으면 fleet scope, 없으면 tenant 글로벌 — admin만 통과.
  const canDismiss = useHasPermission('insight', 'write', insight.fleetId ?? undefined)
  const isOffline = useIsOffline()

  // D-UI-1 Stage 4 — window.prompt → ConfirmDialog 교체.
  //   irreversible action이므로 destructive 확인 후 default reason('manual review')으로 dismiss.
  //   상세 사유 입력은 후속 task에서 별도 dialog로 확장 (현재는 ConfirmDialog imperative API).
  const onDismiss = async (): Promise<void> => {
    const ok = await confirm({
      title: t('findings.dismiss.confirm.title'),
      description: t('findings.dismiss.confirm.description'),
      confirmLabel: t('findings.dismiss.confirm.confirm'),
      cancelLabel: t('findings.dismiss.confirm.cancel'),
      destructive: true,
    })
    if (!ok) return
    // D-UI-1 P0 — Undo window: ConfirmDialog 통과 후 5초 보류.
    //   undo 시 mutation 자체를 호출하지 않으므로 optimistic update도 트리거되지
    //   않아 별도 rollback 불필요. delay 후 mutate → optimistic remove +
    //   onSettled invalidate.
    undoableAction({
      message: t('findings.dismiss.toast.success'),
      undoLabel: t('common.undo'),
      action: () =>
        dismiss.mutateAsync({
          insightId: insight.id,
          reason: t('findings.prompt.dismiss.default'),
        }),
      errorLabel: t('findings.error.fallback'),
    })
  }

  const scope: string[] = []
  if (insight.robotId) scope.push(`robot:${insight.robotId}`)
  if (insight.fleetId) scope.push(`fleet:${insight.fleetId}`)
  if (insight.checkId) scope.push(`check:${insight.checkId}`)

  return (
    <TableRow>
      <TableCell>
        <Badge variant="outline">{insight.kind}</Badge>
      </TableCell>
      <TableCell>
        <SeverityBadge severity={toSeverityKey(insight.severity)} />
      </TableCell>
      <TableCell className="max-w-md truncate" title={insight.summary}>
        {insight.summary}
      </TableCell>
      <TableCell className="font-mono text-xs text-muted-foreground">
        {scope.length === 0 ? '-' : scope.join(' · ')}
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {new Date(insight.createdAt).toLocaleString()}
      </TableCell>
      <TableCell className="text-right">
        <Button
          size="sm"
          variant="outline"
          onClick={() => {
            void onDismiss()
          }}
          disabled={dismiss.isPending || !canDismiss || isOffline}
          title={mutationGuardTitle({
            isOffline,
            offlineLabel: t('pwa.offline.mutationBlocked'),
            fallback: !canDismiss ? t('common.role.required.admin') : undefined,
          })}
        >
          {dismiss.isPending
            ? t('findings.action.dismissing')
            : t('findings.action.dismiss')}
        </Button>
      </TableCell>
    </TableRow>
  )
}

// SEVERITY_RANK_DESC — severity 정렬 우선순위 (높을수록 위). filter "all"일 때 자동 적용.
const SEVERITY_RANK_DESC: Record<string, number> = {
  critical: 5,
  high: 4,
  medium: 3,
  low: 2,
  info: 1,
}

// sortInsightsBySeverityIfUnfiltered — severity 필터 미선택 시 critical→info 순 정렬.
// 필터 선택 시는 그대로(서버 응답 순서, 보통 created DESC) 유지.
// 단위 테스트(findings.test.tsx) 대상으로 export.
export function sortInsightsBySeverityIfUnfiltered(
  insights: Insight[],
  activeSeverity: string,
): Insight[] {
  if (activeSeverity) return insights
  return [...insights].sort((a, b) => {
    const ra = SEVERITY_RANK_DESC[a.severity] ?? 0
    const rb = SEVERITY_RANK_DESC[b.severity] ?? 0
    if (ra !== rb) return rb - ra
    // 동일 severity → createdAt DESC (서버 기본 순서 유지)
    return new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
  })
}

// SeverityStats — severity별 카운트 카드 5개. 클릭 시 해당 severity 필터 toggle.
// active(현재 필터 값)와 동일 severity 카드는 ring으로 강조.
function SeverityStats({
  insights,
  onClick,
  active,
}: {
  insights: Insight[]
  onClick: (severity: string) => void
  active: string
}): React.ReactElement {
  const t = useT()
  const counts: Record<string, number> = {
    critical: 0,
    high: 0,
    medium: 0,
    low: 0,
    info: 0,
  }
  for (const ins of insights) {
    if (counts[ins.severity] !== undefined) {
      counts[ins.severity]!++
    }
  }
  // 표시 순서: critical → info (위험도 desc). label은 i18n severity dict 사용.
  const order: Array<{ severity: Severity; bg: string; text: string }> = [
    { severity: 'critical', bg: 'bg-destructive/10', text: 'text-destructive' },
    { severity: 'high', bg: 'bg-destructive/10', text: 'text-destructive' },
    { severity: 'medium', bg: 'bg-primary/10', text: 'text-primary' },
    { severity: 'low', bg: 'bg-yellow-500/10', text: 'text-yellow-700 dark:text-yellow-400' },
    { severity: 'info', bg: 'bg-muted', text: 'text-muted-foreground' },
  ]
  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-5">
      {order.map((o) => {
        const isActive = active === o.severity
        const count = counts[o.severity] ?? 0
        return (
          <button
            key={o.severity}
            type="button"
            onClick={() => onClick(isActive ? '' : o.severity)}
            aria-pressed={isActive}
            aria-label={t('findings.stats.toggle', { severity: o.severity, count: count.toString() })}
            className={`flex flex-col items-start gap-1 rounded-md border px-3 py-2 text-left transition-all hover:border-foreground/40 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring ${o.bg} ${
              isActive ? 'ring-2 ring-foreground/40' : ''
            }`}
          >
            <span className={`text-xs font-medium ${o.text}`}>{t(`severity.${o.severity}` as never)}</span>
            <span className="text-2xl font-bold leading-none">{count}</span>
          </button>
        )
      })}
    </div>
  )
}

// severityVariant는 Insight severity(5-값)를 shadcn Badge variant로 매핑합니다.
// SeverityBadge 적용 이후 row 셀은 본 함수를 직접 사용하지 않으나, test 호환 유지 위해 export.
// 단위 테스트(findings.test.tsx) 대상으로 export.
export function severityVariant(
  s: string,
): 'default' | 'destructive' | 'secondary' | 'outline' {
  switch (s) {
    case 'critical':
    case 'high':
      return 'destructive'
    case 'medium':
      return 'default'
    case 'low':
    case 'info':
    default:
      return 'secondary'
  }
}

// buildInsightsFilter는 페이지의 3개 입력 상태를 useInsights에 넘길 filter 객체로 변환합니다.
// 빈 값 필드는 객체에서 빠짐(서버 측에서 filter 미적용으로 해석).
// 단위 테스트(findings.test.tsx) 대상으로 export.
export function buildInsightsFilter(input: {
  kind: string
  severity: string
  robotId: string
}): { kind?: string; severity?: string; robotId?: string } {
  return {
    ...(input.kind ? { kind: input.kind } : {}),
    ...(input.severity ? { severity: input.severity } : {}),
    ...(input.robotId.trim() ? { robotId: input.robotId.trim() } : {}),
  }
}

export const Route = createFileRoute('/_authenticated/findings')({
  component: FindingsPage,
})
