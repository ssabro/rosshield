import { createFileRoute, useNavigate, useSearch } from '@tanstack/react-router'
import { useState } from 'react'

import { ApiError } from '@/api/errors'
import {
  isTerminalScanStatus,
  useCancelScan,
  useFleets,
  useIsAdmin,
  usePacks,
  useScan,
  useScanProgress,
  useScans,
  useStartScan,
} from '@/api/hooks'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { ScanSession } from '@/api/hooks'
import type { FormEvent } from 'react'

// `/scans` — 새 스캔 시작 폼.
// - 별도 목록 endpoint가 Stage B에 없어, Phase 1은 시작 폼 + 결과 카드만 노출.
// - 성공 시 sessionId·status 카드 표시. 실패 시 에러 메시지.
const TRIGGERS = ['manual', 'schedule', 'event'] as const

function ScansPage(): React.ReactElement {
  const [fleetId, setFleetId] = useState('')
  const [packId, setPackId] = useState('')
  const [trigger, setTrigger] = useState<'manual' | 'schedule' | 'event'>('manual')
  const [error, setError] = useState('')
  const t = useT()
  const isAdmin = useIsAdmin()
  const packsQuery = usePacks()
  const navigate = useNavigate()
  // URL ?session=<id>로 마지막 세션 보존 (페이지 reload 후에도 진행 카드 복원).
  const search = useSearch({ from: '/_authenticated/scans' }) as {
    session?: string
  }
  const activeSessionId = search.session

  const fleetsForForm = useFleets()
  const startScan = useStartScan()

  const handleSubmit = (e: FormEvent<HTMLFormElement>): void => {
    e.preventDefault()
    setError('')
    startScan.mutate(
      { fleetId, packId, trigger },
      {
        onSuccess: (session) => {
          void navigate({
            to: '/scans',
            search: { session: session.sessionId },
            replace: true,
          })
        },
        onError: (err) => {
          if (err instanceof ApiError) {
            // 같은 fleet에 이미 active 세션이 있는 경우 — 친화 메시지로 안내.
            if (err.status === 409) {
              setError(t('scans.form.error.fleetActive'))
            } else {
              setError(err.message)
            }
          } else {
            setError(err instanceof Error ? err.message : t('scans.form.error.fallback'))
          }
        },
      },
    )
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('pages.scans.title')}
        description={t('pages.scans.description')}
      />

      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle>{t('scans.form.title')}</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="fleetId">{t('scans.form.fleet')}</Label>
              {fleetsForForm.isPending ? (
                <Input
                  id="fleetId"
                  disabled
                  placeholder={t('scans.form.fleet.loading')}
                />
              ) : fleetsForForm.isError ||
                (fleetsForForm.data?.length ?? 0) === 0 ? (
                <Input
                  id="fleetId"
                  required
                  value={fleetId}
                  onChange={(e) => setFleetId(e.target.value)}
                  placeholder={t('scans.form.fleet.placeholder')}
                />
              ) : (
                <Select value={fleetId} onValueChange={setFleetId}>
                  <SelectTrigger id="fleetId">
                    <SelectValue placeholder={t('scans.form.fleet.placeholder')} />
                  </SelectTrigger>
                  <SelectContent>
                    {fleetsForForm.data?.map((fl) => (
                      <SelectItem key={fl.id} value={fl.id}>
                        {fl.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="packId">{t('scans.form.pack')}</Label>
              {packsQuery.isPending ? (
                <Input
                  id="packId"
                  disabled
                  placeholder={t('scans.form.pack.loading')}
                />
              ) : packsQuery.isError || (packsQuery.data?.length ?? 0) === 0 ? (
                <Input
                  id="packId"
                  required
                  value={packId}
                  onChange={(e) => setPackId(e.target.value)}
                  placeholder={t('scans.form.pack.placeholder')}
                />
              ) : (
                <Select value={packId} onValueChange={setPackId}>
                  <SelectTrigger id="packId">
                    <SelectValue placeholder={t('scans.form.pack.placeholder')} />
                  </SelectTrigger>
                  <SelectContent>
                    {packsQuery.data?.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {p.name} ({p.version}){p.isBuiltin ? ' · built-in' : ''}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="trigger">{t('scans.form.trigger')}</Label>
              <Select
                value={trigger}
                onValueChange={(v) => setTrigger(v as 'manual' | 'schedule' | 'event')}
              >
                <SelectTrigger id="trigger">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {TRIGGERS.map((tr) => (
                    <SelectItem key={tr} value={tr}>
                      {tr}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {error && (
              <p className="text-sm text-destructive" role="alert">
                {error}
              </p>
            )}
            <Button
              type="submit"
              disabled={startScan.isPending || !isAdmin}
              title={!isAdmin ? t('common.role.required.admin') : undefined}
            >
              {startScan.isPending
                ? t('scans.form.submitting')
                : t('scans.form.submit')}
            </Button>
          </form>
        </CardContent>
      </Card>

      {activeSessionId && <SessionProgressCardById sessionId={activeSessionId} />}

      <RecentSessionsCard activeSessionId={activeSessionId} />
    </div>
  )
}

// STATUS_FILTER_VALUES는 Status dropdown의 표시 항목입니다.
// 'all' sentinel은 client-side에서 필터 미적용을 의미.
const STATUS_FILTER_VALUES = [
  'all',
  'pending',
  'running',
  'completed',
  'failed',
  'cancelled',
] as const
type StatusFilterValue = (typeof STATUS_FILTER_VALUES)[number]

// FLEET_ALL_VALUE는 fleet dropdown의 'all' sentinel입니다.
const FLEET_ALL_VALUE = '__all__'

// RecentSessionsCard는 최근 세션 10개를 표 형태로 표시합니다.
// active 세션(pending/running) 1건 이상이면 5s polling — terminal 도달 시 정지.
// 행 클릭 시 ?session=<id>로 navigate해 위 진행 카드에 즉시 표시.
//
// 필터: status + fleet dropdown (client-side, 10개 max라 부담 X).
// fleet 옵션은 현재 세션 목록에서 distinct fleetId 추출 — 별 endpoint 없이 즉시.
function RecentSessionsCard({
  activeSessionId,
}: {
  activeSessionId?: string
}): React.ReactElement {
  const t = useT()
  const navigate = useNavigate()
  const scansQuery = useScans({ limit: 10, pollMs: 5000 })
  const fleetsQuery = useFleets()
  const [statusFilter, setStatusFilter] = useState<StatusFilterValue>('all')
  const [fleetFilter, setFleetFilter] = useState<string>(FLEET_ALL_VALUE)

  if (scansQuery.isPending) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{t('scans.list.title')}</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          {t('scans.list.loading')}
        </CardContent>
      </Card>
    )
  }
  if (scansQuery.isError) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{t('scans.list.title')}</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-destructive">
          {scansQuery.error instanceof Error
            ? scansQuery.error.message
            : t('scans.list.error')}
        </CardContent>
      </Card>
    )
  }

  const list = scansQuery.data ?? []
  // useFleets로 전체 활성 fleets를 직접 조회 — 세션 distinct 추출보다 정확.
  // fetch 실패 시 fallback: 세션 distinct (orphan fleet 노출은 X — 본 카드 fleet만 보여줌).
  const fleetOptions =
    fleetsQuery.data && fleetsQuery.data.length > 0
      ? fleetsQuery.data.map((f) => ({ id: f.id, name: f.name }))
      : Array.from(new Set(list.map((s) => s.fleetId)))
          .sort()
          .map((id) => ({ id, name: id }))
  const filtered = list.filter((s) => {
    if (statusFilter !== 'all' && s.status !== statusFilter) return false
    if (fleetFilter !== FLEET_ALL_VALUE && s.fleetId !== fleetFilter) return false
    return true
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('scans.list.title')}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {list.length > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="filter-status" className="text-xs text-muted-foreground">
                {t('scans.list.filter.status')}
              </Label>
              <Select
                value={statusFilter}
                onValueChange={(v) => setStatusFilter(v as StatusFilterValue)}
              >
                <SelectTrigger id="filter-status" className="h-8 w-[140px] text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {STATUS_FILTER_VALUES.map((v) => (
                    <SelectItem key={v} value={v}>
                      {v === 'all' ? t('scans.list.filter.all') : v}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex items-center gap-1.5">
              <Label htmlFor="filter-fleet" className="text-xs text-muted-foreground">
                {t('scans.list.filter.fleet')}
              </Label>
              <Select value={fleetFilter} onValueChange={setFleetFilter}>
                <SelectTrigger id="filter-fleet" className="h-8 w-[180px] text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={FLEET_ALL_VALUE}>
                    {t('scans.list.filter.all')}
                  </SelectItem>
                  {fleetOptions.map((opt) => (
                    <SelectItem key={opt.id} value={opt.id}>
                      {opt.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <span className="ml-auto text-xs text-muted-foreground">
              {t('scans.list.count', {
                shown: filtered.length,
                total: list.length,
              })}
            </span>
          </div>
        )}
        {list.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('scans.list.empty')}</p>
        ) : filtered.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {t('scans.list.noMatches')}
          </p>
        ) : (
          <div className="space-y-1">
            {filtered.map((s) => (
              <SessionRow
                key={s.sessionId}
                session={s}
                isActive={s.sessionId === activeSessionId}
                onSelect={() =>
                  void navigate({
                    to: '/scans',
                    search: { session: s.sessionId },
                    replace: true,
                  })
                }
              />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function SessionRow({
  session,
  isActive,
  onSelect,
}: {
  session: ScanSession
  isActive: boolean
  onSelect: () => void
}): React.ReactElement {
  const t = useT()
  const total = session.total
  const completed = session.completed
  const percent = total > 0 ? Math.min(100, Math.round((completed / total) * 100)) : 0
  return (
    <button
      type="button"
      onClick={onSelect}
      className={`w-full rounded border px-3 py-2 text-left text-sm transition hover:bg-accent ${
        isActive ? 'border-primary bg-accent/50' : 'border-border'
      }`}
    >
      <div className="flex items-center gap-2">
        <span className="truncate font-mono text-xs">{session.sessionId}</span>
        <Badge variant={statusVariant(session.status)}>{session.status}</Badge>
        <span className="ml-auto text-xs text-muted-foreground">
          {formatRelativeTime(session.createdAt)}
        </span>
      </div>
      <div className="mt-1 flex items-center justify-between text-xs text-muted-foreground">
        <span>
          fleet=<span className="font-mono">{session.fleetId}</span> ·{' '}
          {completed}/{total}
          {session.failed > 0
            ? ` (${t('scans.session.failed', { count: session.failed })})`
            : ''}
        </span>
        <span>{percent}%</span>
      </div>
      <SessionSeverityRow session={session} />
    </button>
  )
}

// SessionSeverityCardGrid — single session detail card에 4 severity 카드형 분포 표시.
//
// SessionProgressCard 안에서 terminal 도달 후만 렌더(전 단계는 0으로 의미 없음). list view의
// SessionSeverityRow가 inline pill compact 표현이라면, 본 카드는 packs.SeverityStats 패턴
// 재사용한 풍부한 표시 — severity 라벨 + 카운트 + 색상 톤. 클릭 토글은 미제공(D26 §5.6,
// 본 카드는 read-only summary).
function SessionSeverityCardGrid({
  session,
}: {
  session: ScanSession
}): React.ReactElement {
  const t = useT()
  const order: Array<{ severity: string; count: number; bg: string; text: string }> = [
    { severity: 'critical', count: session.severityCriticalFailed, bg: 'bg-destructive/10', text: 'text-destructive' },
    { severity: 'high', count: session.severityHighFailed, bg: 'bg-destructive/10', text: 'text-destructive' },
    { severity: 'medium', count: session.severityMediumFailed, bg: 'bg-primary/10', text: 'text-primary' },
    { severity: 'low', count: session.severityLowFailed, bg: 'bg-muted', text: 'text-muted-foreground' },
  ]
  return (
    <div>
      <div className="mb-1.5 text-xs text-muted-foreground">
        {t('scans.session.severity.title')}
      </div>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        {order.map((o) => (
          <div
            key={o.severity}
            className={`flex flex-col items-start gap-0.5 rounded-md border px-2.5 py-1.5 ${o.bg}`}
            aria-label={t('scans.session.severity.tooltip', {
              severity: o.severity,
              count: o.count.toString(),
            })}
          >
            <span className={`text-[10px] font-medium uppercase ${o.text}`}>
              {o.severity}
            </span>
            <span className="text-xl font-bold leading-none tabular-nums">{o.count}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

// SessionSeverityRow — terminal session 4 severity별 fail 카운트를 inline pill로 표시.
//
// pending/running 세션은 모두 0이므로 출력 생략(noise 회피). >0인 severity는 색상 강조,
// 0인 severity는 muted. packs.SeverityStats 패턴 일관(D26-4) — 다만 list view라 카드형 X,
// 작은 pill만. 클릭 토글은 detail 페이지 필터로 충분(D26 §5.6).
export function SessionSeverityRow({
  session,
}: {
  session: ScanSession
}): React.ReactElement | null {
  const t = useT()
  const counts: Array<[string, number]> = [
    ['critical', session.severityCriticalFailed],
    ['high', session.severityHighFailed],
    ['medium', session.severityMediumFailed],
    ['low', session.severityLowFailed],
  ]
  const total = counts.reduce((sum, [, n]) => sum + n, 0)
  // terminal(완료/실패/취소) 외 session은 0뿐이라 noise — 출력 생략.
  if (total === 0) return null
  const tone: Record<string, string> = {
    critical: 'border-destructive/40 bg-destructive/10 text-destructive',
    high: 'border-destructive/40 bg-destructive/10 text-destructive',
    medium: 'border-primary/40 bg-primary/10 text-primary',
    low: 'border-border bg-muted text-muted-foreground',
  }
  const muted = 'border-border bg-transparent text-muted-foreground/60'
  return (
    <div className="mt-1.5 flex flex-wrap items-center gap-1">
      {counts.map(([sev, count]) => (
        <span
          key={sev}
          className={`inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[10px] font-medium uppercase ${
            count > 0 ? tone[sev] : muted
          }`}
          title={t('scans.session.severity.tooltip', {
            severity: sev,
            count: count.toString(),
          })}
        >
          <span>{t(`scans.session.severity.${sev}` as 'scans.session.severity.critical')}</span>
          <span className="text-[11px] tabular-nums">{count}</span>
        </span>
      ))}
    </div>
  )
}

function formatRelativeTime(iso?: string): string {
  if (!iso) return ''
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  const diffMs = Date.now() - t
  const sec = Math.round(diffMs / 1000)
  if (sec < 60) return `${sec}s`
  const min = Math.round(sec / 60)
  if (min < 60) return `${min}m`
  const hr = Math.round(min / 60)
  if (hr < 24) return `${hr}h`
  const day = Math.round(hr / 24)
  return `${day}d`
}

// SessionProgressCardById는 URL의 sessionId로 세션을 fetch한 뒤 진행 카드를 보여줍니다.
// terminal 도달까지 polling은 useScanProgress가 담당 — 본 fetch는 초기 메타(fleetId 등)
// 복원과 WS 미접속 윈도 동안의 첫 표시값 제공이 목적.
function SessionProgressCardById({
  sessionId,
}: {
  sessionId: string
}): React.ReactElement {
  const t = useT()
  const scanQuery = useScan(sessionId)
  if (scanQuery.isPending) {
    return (
      <Card className="max-w-xl">
        <CardContent className="py-6 text-sm text-muted-foreground">
          {t('scans.session.loading')}
        </CardContent>
      </Card>
    )
  }
  if (scanQuery.isError || !scanQuery.data) {
    return (
      <Card className="max-w-xl">
        <CardContent className="py-6 text-sm text-destructive">
          {t('scans.session.notFound')}
        </CardContent>
      </Card>
    )
  }
  return <SessionProgressCard session={scanQuery.data} />
}

function SessionProgressCard({
  session,
}: {
  session: ScanSession
}): React.ReactElement {
  // 진행 추적: WebSocket → 실패 시 자동 polling fallback (useScanProgress 내부에서 처리).
  // 초기 세션 값을 backstop으로 두고 latest 메시지가 도착하면 갱신.
  const ws = useScanProgress(session.sessionId)
  const t = useT()
  const isAdmin = useIsAdmin()
  const cancelScan = useCancelScan()
  // terminal 도달 후 progress 카드 자체가 fresh fetch가 필요할 수 있으므로
  // 백스톱 polling 별도(useScan)는 안 둔다 — useScanProgress의 polling fallback이 처리.

  const total = ws.latest?.total ?? session.total
  const completed = ws.latest?.completed ?? session.completed
  const failed = ws.latest?.failed ?? session.failed
  const status = ws.latest?.status ?? session.status
  const percent = total > 0 ? Math.min(100, Math.round((completed / total) * 100)) : 0
  const isTerminal = isTerminalScanStatus(status)
  const canCancel = !isTerminal && isAdmin

  const handleCancel = (): void => {
    if (!canCancel) return
    cancelScan.mutate({ sessionId: session.sessionId })
  }

  return (
    <Card className="max-w-xl">
      <CardHeader>
        <CardTitle>{t('scans.session.title')}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <div>
          <span className="text-muted-foreground">{t('scans.session.id')}: </span>
          <span className="font-mono">{session.sessionId}</span>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground">{t('scans.session.status')}:</span>
          <Badge variant={statusVariant(status)}>{status}</Badge>
          <Badge variant="outline" className="ml-auto text-xs">
            {sourceLabel(ws.status, isTerminal, t)}
          </Badge>
        </div>
        <div>
          <Progress value={percent} className="h-2" />
          <div className="mt-1 flex items-center justify-between text-xs text-muted-foreground">
            <span>
              {completed} / {total} ({t('scans.session.failed', { count: failed })})
            </span>
            <span>{percent}%</span>
          </div>
          {(() => {
            const eta = computeETA(status, session.startedAt, completed, total)
            return eta ? (
              <p className="mt-1 text-xs text-muted-foreground">
                {t('scans.session.eta', { eta })}
              </p>
            ) : null
          })()}
        </div>
        {isTerminal && <SessionSeverityCardGrid session={session} />}
        {ws.error && <p className="text-xs text-destructive">{ws.error}</p>}
        {!isTerminal && (
          <div className="flex items-center justify-between pt-2">
            <Button
              variant="destructive"
              size="sm"
              onClick={handleCancel}
              disabled={!canCancel || cancelScan.isPending}
              title={
                !isAdmin
                  ? t('common.role.required.admin')
                  : cancelScan.isPending
                    ? t('scans.session.cancel.pending')
                    : undefined
              }
            >
              {cancelScan.isPending
                ? t('scans.session.cancel.pending')
                : t('scans.session.cancel')}
            </Button>
            {cancelScan.isError && (
              <span className="text-xs text-destructive">
                {cancelScan.error instanceof Error
                  ? cancelScan.error.message
                  : t('scans.session.cancel.error')}
              </span>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function sourceLabel(
  wsStatus: ReturnType<typeof useScanProgress>['status'],
  isTerminal: boolean,
  t: ReturnType<typeof useT>,
): string {
  if (isTerminal) return t('scans.session.source.final')
  switch (wsStatus) {
    case 'streaming':
      return t('scans.session.source.live')
    case 'polling':
      return t('scans.session.source.polling')
    case 'connecting':
      return t('scans.session.source.connecting')
    case 'error':
      return t('scans.session.source.error')
    case 'completed':
      return t('scans.session.source.final')
    default:
      return t('scans.session.source.idle')
  }
}

// computeETA — running scan의 estimated time remaining 계산.
//
// 공식: avgPerCheck = elapsed / completed, remaining = (total - completed) * avgPerCheck.
// 조건 (모두 true일 때만 ETA 노출):
//   - status === 'running'
//   - startedAt 존재 + valid date
//   - completed > 0 (분모 0 회피, 첫 check 끝나야 추정 가능)
//   - completed < total (이미 완료면 ETA 무의미)
//   - elapsed > 1초 (너무 짧으면 추정 부정확)
//
// 출력: "1m 23s" / "45s" / "1h 5m" 형식. 단위 테스트 대상으로 export.
export function computeETA(
  status: string,
  startedAt: string | null | undefined,
  completed: number,
  total: number,
): string | null {
  if (status !== 'running') return null
  if (!startedAt) return null
  if (completed <= 0 || completed >= total) return null
  const startMs = new Date(startedAt).getTime()
  if (Number.isNaN(startMs)) return null
  const elapsedMs = Date.now() - startMs
  if (elapsedMs < 1000) return null
  const avgMs = elapsedMs / completed
  const remainingMs = (total - completed) * avgMs
  return formatDuration(remainingMs)
}

// formatDuration — milliseconds를 short form(`Nh Nm` / `Nm Ns` / `Ns`)으로 변환.
function formatDuration(ms: number): string {
  const totalSec = Math.max(1, Math.round(ms / 1000))
  if (totalSec < 60) return `${totalSec}s`
  const min = Math.floor(totalSec / 60)
  const sec = totalSec % 60
  if (min < 60) {
    return sec > 0 ? `${min}m ${sec}s` : `${min}m`
  }
  const hr = Math.floor(min / 60)
  const remMin = min % 60
  return remMin > 0 ? `${hr}h ${remMin}m` : `${hr}h`
}

function statusVariant(
  status: string,
): 'default' | 'destructive' | 'secondary' | 'outline' {
  switch (status) {
    case 'completed':
      return 'default'
    case 'failed':
    case 'cancelled':
      return 'destructive'
    case 'running':
    case 'pending':
      return 'secondary'
    default:
      return 'outline'
  }
}

export const Route = createFileRoute('/_authenticated/scans')({
  component: ScansPage,
  validateSearch: (search: Record<string, unknown>): { session?: string } => {
    const s = typeof search.session === 'string' ? search.session : undefined
    return s ? { session: s } : {}
  },
})
