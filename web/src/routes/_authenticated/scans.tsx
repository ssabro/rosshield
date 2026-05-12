import { createFileRoute, useNavigate, useSearch } from '@tanstack/react-router'
import { useState } from 'react'

import { ApiError } from '@/api/errors'
import {
  isTerminalScanStatus,
  useIsAdmin,
  usePacks,
  useScan,
  useScanProgress,
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
            setError(err.message)
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
              <Input
                id="fleetId"
                required
                value={fleetId}
                onChange={(e) => setFleetId(e.target.value)}
                placeholder={t('scans.form.fleet.placeholder')}
              />
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
    </div>
  )
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
  // terminal 도달 후 progress 카드 자체가 fresh fetch가 필요할 수 있으므로
  // 백스톱 polling 별도(useScan)는 안 둔다 — useScanProgress의 polling fallback이 처리.

  const total = ws.latest?.total ?? session.total
  const completed = ws.latest?.completed ?? session.completed
  const failed = ws.latest?.failed ?? session.failed
  const status = ws.latest?.status ?? session.status
  const percent = total > 0 ? Math.min(100, Math.round((completed / total) * 100)) : 0
  const isTerminal = isTerminalScanStatus(status)

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
        </div>
        {ws.error && <p className="text-xs text-destructive">{ws.error}</p>}
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
