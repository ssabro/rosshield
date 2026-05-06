import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { Inbox } from 'lucide-react'

import { ApiError } from '@/api/errors'
import { useDismissInsight, useInsights } from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
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

  const filter = buildInsightsFilter({ kind, severity, robotId })
  const insights = useInsights(filter)

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Findings</h1>
        <p className="text-sm text-muted-foreground">
          drift·anomaly·peer detector가 산출한 활성 Insight입니다. 자동 생성은 scan 완료
          시 일어나며, 수동으로 dismiss하면 활성 목록에서 사라집니다.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <div className="flex flex-col gap-2">
          <Label htmlFor="kind-filter">Kind</Label>
          <Select value={kind || 'all'} onValueChange={(v) => setKind(v === 'all' ? '' : v)}>
            <SelectTrigger id="kind-filter">
              <SelectValue placeholder="전체" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">전체</SelectItem>
              {KIND_OPTIONS.map((k) => (
                <SelectItem key={k} value={k}>
                  {k}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="severity-filter">Severity</Label>
          <Select value={severity || 'all'} onValueChange={(v) => setSeverity(v === 'all' ? '' : v)}>
            <SelectTrigger id="severity-filter">
              <SelectValue placeholder="전체" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">전체</SelectItem>
              {SEVERITY_OPTIONS.map((s) => (
                <SelectItem key={s} value={s}>
                  {s}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="robot-filter">Robot ID</Label>
          <Input
            id="robot-filter"
            placeholder="예: ro_ABC..."
            value={robotId}
            onChange={(e) => setRobotId(e.target.value)}
          />
        </div>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Kind</TableHead>
              <TableHead>Severity</TableHead>
              <TableHead>Summary</TableHead>
              <TableHead>Scope</TableHead>
              <TableHead>생성</TableHead>
              <TableHead className="text-right">조치</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {insights.isPending && (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-muted-foreground">
                  불러오는 중…
                </TableCell>
              </TableRow>
            )}
            {insights.isError && (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-destructive">
                  {insights.error instanceof ApiError
                    ? insights.error.message
                    : 'Insight 목록을 불러올 수 없습니다'}
                </TableCell>
              </TableRow>
            )}
            {insights.isSuccess && insights.data.length === 0 && (
              <TableRow>
                <TableCell colSpan={6} className="p-0">
                  <EmptyState
                    icon={Inbox}
                    title="활성 Insight가 없습니다"
                    description="필터를 비우거나, scan 완료 후 자동 산출되거나 fleet 단위로 :run을 호출하세요."
                    className="rounded-none border-0 bg-transparent"
                  />
                </TableCell>
              </TableRow>
            )}
            {insights.isSuccess &&
              insights.data.map((ins) => <InsightRow key={ins.id} insight={ins} />)}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function InsightRow({ insight }: { insight: Insight }): React.ReactElement {
  const dismiss = useDismissInsight()

  const onDismiss = (): void => {
    const reason = window.prompt('Dismiss 사유를 입력하세요', 'manual review')
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
        {new Date(insight.createdAt).toLocaleString('ko-KR')}
      </TableCell>
      <TableCell className="text-right">
        <Button
          size="sm"
          variant="outline"
          onClick={onDismiss}
          disabled={dismiss.isPending}
        >
          {dismiss.isPending ? '처리 중…' : 'Dismiss'}
        </Button>
      </TableCell>
    </TableRow>
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
