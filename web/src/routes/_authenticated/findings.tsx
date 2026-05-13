import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { Inbox } from 'lucide-react'

import { ApiError } from '@/api/errors'
import { useDismissInsight, useInsights, useIsAdmin } from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type { Insight } from '@/api/hooks'

// `/findings` — Insight 목록 (E19-1).
// - kind/severity/robotId 필터 (모두 선택 옵션)
// - 각 row의 "Dismiss" 버튼 → reason 입력 후 API 호출 → 자동 invalidate
// - 빈 결과/로딩/에러는 robots.tsx와 동일 패턴
const KIND_OPTIONS = ['drift', 'anomaly', 'peer', 'root_cause', 'prediction'] as const
const SEVERITY_OPTIONS = ['info', 'low', 'medium', 'high', 'critical'] as const

function FindingsPage(): React.ReactElement {
  const [kind, setKind] = useState<string>('')
  const [severity, setSeverity] = useState<string>('')
  const [robotId, setRobotId] = useState<string>('')
  const t = useT()

  const filter = buildInsightsFilter({ kind, severity, robotId })
  const insights = useInsights(filter)

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

      <div className="rounded-md border">
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
                <TableCell colSpan={6} className="text-center text-muted-foreground">
                  {t('common.loading')}
                </TableCell>
              </TableRow>
            )}
            {insights.isError && (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-destructive">
                  {insights.error instanceof ApiError
                    ? insights.error.message
                    : t('findings.error.fallback')}
                </TableCell>
              </TableRow>
            )}
            {insights.isSuccess && insights.data.length === 0 && (
              <TableRow>
                <TableCell colSpan={6} className="p-0">
                  <EmptyState
                    icon={Inbox}
                    title={t('findings.empty.title')}
                    description={t('findings.empty.description')}
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
  )
}

function InsightRow({ insight }: { insight: Insight }): React.ReactElement {
  const dismiss = useDismissInsight()
  const t = useT()
  const isAdmin = useIsAdmin()

  const onDismiss = (): void => {
    const reason = window.prompt(
      t('findings.prompt.dismiss'),
      t('findings.prompt.dismiss.default'),
    )
    if (!reason || reason.trim().length === 0) return
    dismiss.mutate({ insightId: insight.id, reason: reason.trim() })
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
        <Badge variant={severityVariant(insight.severity)}>{insight.severity}</Badge>
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
          onClick={onDismiss}
          disabled={dismiss.isPending || !isAdmin}
          title={!isAdmin ? t('common.role.required.admin') : undefined}
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
  // 표시 순서: critical → info (위험도 desc)
  const order: Array<{ severity: string; label: string; bg: string; text: string }> = [
    { severity: 'critical', label: 'critical', bg: 'bg-destructive/10', text: 'text-destructive' },
    { severity: 'high', label: 'high', bg: 'bg-destructive/10', text: 'text-destructive' },
    { severity: 'medium', label: 'medium', bg: 'bg-primary/10', text: 'text-primary' },
    { severity: 'low', label: 'low', bg: 'bg-yellow-500/10', text: 'text-yellow-700 dark:text-yellow-400' },
    { severity: 'info', label: 'info', bg: 'bg-muted', text: 'text-muted-foreground' },
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
            <span className={`text-xs font-medium uppercase ${o.text}`}>{o.label}</span>
            <span className="text-2xl font-bold leading-none">{count}</span>
          </button>
        )
      })}
    </div>
  )
}

// severityVariant는 Insight severity(5-값)를 shadcn Badge variant로 매핑합니다.
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
