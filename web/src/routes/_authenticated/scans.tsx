import { createFileRoute, Link, useNavigate, useSearch } from '@tanstack/react-router'
import { Plus } from 'lucide-react'
import { useState } from 'react'

import { ApiError } from '@/api/errors'
import {
  isTerminalScanStatus,
  useCancelScan,
  useFleets,
  useHasPermission,
  usePacks,
  useScan,
  useScanProgress,
  useScans,
  useStartScan,
} from '@/api/hooks'
import { StatusBadge, type StatusKind } from '@/components/common/StatusBadge'
import { TruncatedId } from '@/components/common/TruncatedId'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { confirm } from '@/lib/confirm'
import { toast } from '@/lib/toast'
import { mutationGuardTitle, useIsOffline } from '@/lib/use-is-offline'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
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
import { Skeleton, TableRowSkeleton } from '@/components/ui/skeleton'

import type { ScanSession } from '@/api/hooks'
import type { FormEvent } from 'react'

// `/scans` — D-UI-2 리팩토링.
//
// 기본 view = 최근 세션 테이블 + 우측 상단 "+ 새 스캔" 버튼.
// 패턴 (사용자 spec):
//   1. List + Create Dialog  — "+ 새 스캔" → Dialog 안 form → submit 후 dialog close + toast
//   2. List + Detail Dialog  — row click → SessionDetail Dialog (live progress + cancel)
//   3. ID Truncate           — sessionId/fleetId → TruncatedId 컴포넌트
//
// URL state: `?session=<id>` 보존 (deep link · reload · 다른 탭 공유 호환). dialog open
// 상태는 session search param 유무로 도출 — 별도 useState 0.
const TRIGGERS = ['manual', 'schedule', 'event'] as const

function ScansPage(): React.ReactElement {
  const t = useT()
  const navigate = useNavigate()
  const [createOpen, setCreateOpen] = useState(false)
  // URL ?session=<id>로 선택 세션 보존 — dialog open 상태가 URL에 종속(deep link 가능).
  const search = useSearch({ from: '/_authenticated/scans' }) as {
    session?: string
  }
  const activeSessionId = search.session
  const isOffline = useIsOffline()
  // RBAC Stage 5 — start scan은 scan.execute. fleet 선택 없을 때(form 진입 전) admin
  // tenant scope만 통과 — 비-admin은 dialog open trigger도 disabled.
  const canStartAny = useHasPermission('scan', 'execute')

  const closeSessionDialog = (): void => {
    void navigate({
      to: '/scans',
      search: {},
      replace: true,
    })
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('pages.scans.title')}
        description={t('pages.scans.description')}
        actions={
          <Button
            size="sm"
            onClick={() => setCreateOpen(true)}
            disabled={!canStartAny || isOffline}
            title={mutationGuardTitle({
              isOffline,
              offlineLabel: t('pwa.offline.mutationBlocked'),
              fallback: !canStartAny ? t('common.role.required.admin') : undefined,
            })}
          >
            <Plus className="size-4" aria-hidden />
            {t('scans.create.button')}
          </Button>
        }
      />

      <RecentSessionsCard activeSessionId={activeSessionId} />

      {/* 새 스캔 시작 Dialog — 기본 페이지에서 분리 (사용자 spec). */}
      <CreateScanDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(sessionId) => {
          setCreateOpen(false)
          void navigate({
            to: '/scans',
            search: { session: sessionId },
            replace: true,
          })
        }}
      />

      {/* 세션 상세 Dialog — row click 또는 ?session=<id> 진입 시 open. */}
      <SessionDetailDialog
        sessionId={activeSessionId}
        onClose={closeSessionDialog}
      />
    </div>
  )
}

// CreateScanDialog — "+ 새 스캔" 클릭 시 열리는 Dialog. 기존 form 로직 그대로 이동.
//
// onCreated(sessionId) callback: dialog close + URL ?session= 갱신은 부모 책임.
function CreateScanDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (sessionId: string) => void
}): React.ReactElement {
  const t = useT()
  const [fleetId, setFleetId] = useState('')
  const [packId, setPackId] = useState('')
  const [trigger, setTrigger] = useState<'manual' | 'schedule' | 'event'>('manual')
  const [error, setError] = useState('')
  // RBAC: form fleetId 입력에 따라 fleet scope 평가. admin tenant scope는 무관 통과.
  const canStart = useHasPermission(
    'scan',
    'execute',
    fleetId.trim().length > 0 ? fleetId.trim() : undefined,
  )
  const isOffline = useIsOffline()
  const packsQuery = usePacks()
  const fleetsForForm = useFleets()
  const startScan = useStartScan()

  const handleSubmit = (e: FormEvent<HTMLFormElement>): void => {
    e.preventDefault()
    setError('')
    startScan.mutate(
      { fleetId, packId, trigger },
      {
        onSuccess: (session) => {
          toast.success(t('scans.create.toast.success'))
          // 폼 reset → dialog close 후 다음 open이 깨끗한 상태.
          setFleetId('')
          setPackId('')
          setTrigger('manual')
          onCreated(session.sessionId)
        },
        onError: (err) => {
          if (err instanceof ApiError) {
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
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('scans.create.title')}</DialogTitle>
          <DialogDescription>{t('scans.create.description')}</DialogDescription>
        </DialogHeader>
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
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={startScan.isPending}
            >
              {t('common.dialog.cancel')}
            </Button>
            <Button
              type="submit"
              disabled={startScan.isPending || !canStart || isOffline}
              title={mutationGuardTitle({
                isOffline,
                offlineLabel: t('pwa.offline.mutationBlocked'),
                fallback: !canStart
                  ? t('common.role.required.admin')
                  : undefined,
              })}
            >
              {startScan.isPending
                ? t('scans.form.submitting')
                : t('scans.form.submit')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// SessionDetailDialog — row 클릭 시 열리는 세션 상세 modal.
//
// 기존 SessionProgressCard 내용을 dialog body 안으로 옮김. URL ?session=<id>로 deep
// link 보존(reload·다른 탭 공유 가능). open 상태는 sessionId 유무로 도출.
function SessionDetailDialog({
  sessionId,
  onClose,
}: {
  sessionId: string | undefined
  onClose: () => void
}): React.ReactElement {
  const t = useT()
  return (
    <Dialog
      open={Boolean(sessionId)}
      onOpenChange={(next) => {
        if (!next) onClose()
      }}
    >
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('scans.session.title')}</DialogTitle>
        </DialogHeader>
        {sessionId ? (
          <SessionProgressById sessionId={sessionId} />
        ) : null}
      </DialogContent>
    </Dialog>
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

// scanStatusToStatusKind — scan 도메인 status를 공통 StatusBadge 표시값으로 매핑.
// cancelled는 StatusBadge의 직접 매핑 항목이 없어 `paused`(amber) tone으로 대체하되,
// label은 i18n status.cancelled로 override하여 의미는 정확히 표시.
// 단위 테스트(scans.test.tsx) 대상으로 export.
export function scanStatusToStatusKind(status: string): StatusKind {
  switch (status) {
    case 'running':
      return 'running'
    case 'pending':
      return 'pending'
    case 'completed':
      return 'success'
    case 'failed':
      return 'failed'
    case 'cancelled':
      return 'paused'
    default:
      return 'unknown'
  }
}

// scanStatusLabelKey — scan status별 i18n 라벨 키. StatusBadge label override 용.
const SCAN_STATUS_LABEL_KEY: Record<string, 'status.running' | 'status.pending' | 'status.completed' | 'status.failed' | 'status.cancelled'> = {
  running: 'status.running',
  pending: 'status.pending',
  completed: 'status.completed',
  failed: 'status.failed',
  cancelled: 'status.cancelled',
}

function ScanStatusBadge({ status }: { status: string }): React.ReactElement {
  const t = useT()
  const key = SCAN_STATUS_LABEL_KEY[status]
  return (
    <StatusBadge
      status={scanStatusToStatusKind(status)}
      label={key ? t(key) : status}
    />
  )
}

// RecentSessionsCard는 최근 세션 10개를 표 형태로 표시합니다.
// active 세션(pending/running) 1건 이상이면 5s polling — terminal 도달 시 정지.
// 행 클릭 시 ?session=<id>로 navigate해 상세 dialog가 열립니다.
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
    // D-UI-1 Stage 4 — loading text → Skeleton 교체.
    return (
      <Card>
        <CardHeader>
          <CardTitle>{t('scans.list.title')}</CardTitle>
        </CardHeader>
        <CardContent>
          <TableRowSkeleton rows={4} columns={4} />
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
        <CardContent>
          <EmptyState
            variant="loading-fail"
            title={
              scansQuery.error instanceof Error
                ? scansQuery.error.message
                : t('scans.list.error')
            }
            description={t('scans.list.error')}
            className="bg-transparent"
          />
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

  const resetFilters = (): void => {
    setStatusFilter('all')
    setFleetFilter(FLEET_ALL_VALUE)
  }

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
          // D-UI-1 Stage 4 — 세션 0건 → EmptyState + CTA (로봇 등록 안내).
          <EmptyState
            variant="no-data"
            title={t('scans.list.empty')}
            description={t('scans.list.empty.description')}
            action={
              <Button asChild size="sm" variant="outline">
                <Link to="/robots">{t('scans.list.empty.cta')}</Link>
              </Button>
            }
            className="bg-transparent"
          />
        ) : filtered.length === 0 ? (
          <EmptyState
            variant="search-no-result"
            title={t('scans.list.noMatches')}
            action={
              <Button size="sm" variant="outline" onClick={resetFilters}>
                {t('scans.list.noMatches.cta')}
              </Button>
            }
            className="bg-transparent"
          />
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
      <div className="flex flex-wrap items-center gap-2">
        {/* D-UI-2 — 긴 sessionId truncate (prefix scs_ + ellipsis + 마지막 4자). */}
        <TruncatedId id={session.sessionId} />
        <ScanStatusBadge status={session.status} />
        <span className="ml-auto text-xs text-muted-foreground">
          {formatRelativeTime(session.createdAt)}
        </span>
      </div>
      <div className="mt-1 flex items-center justify-between text-xs text-muted-foreground">
        <span className="inline-flex items-center gap-1">
          fleet=
          <TruncatedId id={session.fleetId} prefixLen={3} showCopy={false} />
          <span>
            · {completed}/{total}
            {session.failed > 0
              ? ` (${t('scans.session.failed', { count: session.failed })})`
              : ''}
          </span>
        </span>
        <span>{percent}%</span>
      </div>
      <SessionSeverityRow session={session} />
    </button>
  )
}

// SessionSeverityCardGrid — single session detail card에 4 severity 카드형 분포 표시.
//
// SessionDetailDialog 안에서 terminal 도달 후만 렌더(전 단계는 0으로 의미 없음).
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
            <span className={`text-[10px] font-medium ${o.text}`}>
              {t(`scans.session.severity.${o.severity}` as 'scans.session.severity.critical')}
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
          className={`inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[10px] font-medium ${
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

// SessionProgressById는 URL의 sessionId로 세션을 fetch한 뒤 진행 본문을 보여줍니다.
// terminal 도달까지 polling은 useScanProgress가 담당 — 본 fetch는 초기 메타(fleetId 등)
// 복원과 WS 미접속 윈도 동안의 첫 표시값 제공이 목적.
function SessionProgressById({
  sessionId,
}: {
  sessionId: string
}): React.ReactElement {
  const t = useT()
  const scanQuery = useScan(sessionId)
  if (scanQuery.isPending) {
    // D-UI-1 Stage 4 — loading text → Skeleton 교체.
    return (
      <div className="space-y-3 py-2">
        <Skeleton className="h-4 w-1/2" />
        <Skeleton className="h-4 w-1/3" />
        <Skeleton className="h-2 w-full" />
      </div>
    )
  }
  if (scanQuery.isError || !scanQuery.data) {
    return (
      <EmptyState
        variant="loading-fail"
        title={t('scans.session.notFound')}
        className="bg-transparent"
      />
    )
  }
  return <SessionProgressBody session={scanQuery.data} />
}

function SessionProgressBody({
  session,
}: {
  session: ScanSession
}): React.ReactElement {
  // 진행 추적: WebSocket → 실패 시 자동 polling fallback (useScanProgress 내부에서 처리).
  // 초기 세션 값을 backstop으로 두고 latest 메시지가 도착하면 갱신.
  const ws = useScanProgress(session.sessionId)
  const t = useT()
  // RBAC Stage 5 — cancel은 scan.execute 권한 (§2.2 ID 9).
  //   세션 자체에 fleetId 있음 — fleet 컨텍스트로 정확 평가.
  const canExecute = useHasPermission('scan', 'execute', session.fleetId)
  const isOffline = useIsOffline()
  const cancelScan = useCancelScan()
  // terminal 도달 후 progress 카드 자체가 fresh fetch가 필요할 수 있으므로
  // 백스톱 polling 별도(useScan)는 안 둔다 — useScanProgress의 polling fallback이 처리.

  const total = ws.latest?.total ?? session.total
  const completed = ws.latest?.completed ?? session.completed
  const failed = ws.latest?.failed ?? session.failed
  const status = ws.latest?.status ?? session.status
  const percent = total > 0 ? Math.min(100, Math.round((completed / total) * 100)) : 0
  const isTerminal = isTerminalScanStatus(status)
  const canCancel = !isTerminal && canExecute && !isOffline

  // D-UI-1 Stage 4 — window.confirm 없던 자리에 명시적 ConfirmDialog 추가 (destructive action).
  //   cancel은 진행 중 검사를 즉시 중단하고 부분 결과만 보존 — 사용자 확인 후 실행.
  //   onSuccess/onError에 toast 추가 (silent fail 0건).
  const handleCancel = async (): Promise<void> => {
    if (!canCancel) return
    const ok = await confirm({
      title: t('scans.session.cancel.confirm.title'),
      description: t('scans.session.cancel.confirm.description'),
      confirmLabel: t('scans.session.cancel.confirm.confirm'),
      cancelLabel: t('scans.session.cancel.confirm.cancel'),
      destructive: true,
    })
    if (!ok) return
    cancelScan.mutate(
      { sessionId: session.sessionId },
      {
        onSuccess: () => {
          toast.success(t('scans.session.cancel.toast.success'))
        },
        onError: (err) => {
          toast.error(
            err instanceof Error ? err.message : t('scans.session.cancel.error'),
          )
        },
      },
    )
  }

  return (
    <div className="space-y-3 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-muted-foreground">{t('scans.session.id')}: </span>
        <TruncatedId id={session.sessionId} />
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-muted-foreground">fleet:</span>
        <TruncatedId id={session.fleetId} prefixLen={3} />
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-muted-foreground">{t('scans.session.status')}:</span>
        <ScanStatusBadge status={status} />
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
        <div className="flex flex-wrap items-center justify-between gap-2 pt-2">
          <Button
            variant="destructive"
            size="sm"
            onClick={() => {
              void handleCancel()
            }}
            disabled={!canCancel || cancelScan.isPending}
            title={
              isOffline
                ? t('pwa.offline.mutationBlocked')
                : !canExecute
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
    </div>
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

export const Route = createFileRoute('/_authenticated/scans')({
  component: ScansPage,
  validateSearch: (search: Record<string, unknown>): { session?: string } => {
    const s = typeof search.session === 'string' ? search.session : undefined
    return s ? { session: s } : {}
  },
})
