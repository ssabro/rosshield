import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { ApiError } from '@/api/errors'
import { useScanProgress, useStartScan } from '@/api/hooks'
import { PageHeader } from '@/components/layout/PageHeader'
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
  const [trigger, setTrigger] = useState<string>('manual')
  const [lastSession, setLastSession] = useState<ScanSession | null>(null)
  const [error, setError] = useState('')

  const startScan = useStartScan()

  const handleSubmit = (e: FormEvent<HTMLFormElement>): void => {
    e.preventDefault()
    setError('')
    setLastSession(null)
    startScan.mutate(
      { fleetId, packId, trigger },
      {
        onSuccess: (session) => setLastSession(session),
        onError: (err) => {
          if (err instanceof ApiError) {
            setError(err.message)
          } else {
            setError(err instanceof Error ? err.message : '스캔 시작 실패')
          }
        },
      },
    )
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="스캔"
        description="플릿과 벤치마크 팩을 선택해 새 스캔 세션을 시작합니다."
      />

      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle>새 스캔 시작</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="fleetId">Fleet ID</Label>
              <Input
                id="fleetId"
                required
                value={fleetId}
                onChange={(e) => setFleetId(e.target.value)}
                placeholder="예: production"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="packId">Pack ID</Label>
              <Input
                id="packId"
                required
                value={packId}
                onChange={(e) => setPackId(e.target.value)}
                placeholder="예: cis-ubuntu-24.04"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="trigger">트리거</Label>
              <Select value={trigger} onValueChange={setTrigger}>
                <SelectTrigger id="trigger">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {TRIGGERS.map((t) => (
                    <SelectItem key={t} value={t}>
                      {t}
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
            <Button type="submit" disabled={startScan.isPending}>
              {startScan.isPending ? '시작 중…' : '스캔 시작'}
            </Button>
          </form>
        </CardContent>
      </Card>

      {lastSession && <SessionProgressCard session={lastSession} />}
    </div>
  )
}

function SessionProgressCard({
  session,
}: {
  session: ScanSession
}): React.ReactElement {
  // C1 — WebSocket으로 실시간 진행률 구독. 첫 수신값을 latest, 미수신은 session 초기값 사용.
  const ws = useScanProgress(session.sessionId)

  const total = ws.latest?.total ?? session.total
  const completed = ws.latest?.completed ?? session.completed
  const failed = ws.latest?.failed ?? session.failed
  const status = ws.latest?.status ?? session.status
  const percent = total > 0 ? Math.min(100, Math.round((completed / total) * 100)) : 0

  return (
    <Card className="max-w-xl">
      <CardHeader>
        <CardTitle>시작된 세션</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <div>
          <span className="text-muted-foreground">Session ID: </span>
          <span className="font-mono">{session.sessionId}</span>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground">상태:</span>
          <Badge variant={statusVariant(status)}>{status}</Badge>
          <Badge variant="outline" className="ml-auto text-xs">
            WS {ws.status}
          </Badge>
        </div>
        <div>
          <Progress value={percent} className="h-2" />
          <div className="mt-1 flex items-center justify-between text-xs text-muted-foreground">
            <span>
              {completed} / {total} (실패 {failed})
            </span>
            <span>{percent}%</span>
          </div>
        </div>
        {ws.error && <p className="text-xs text-destructive">{ws.error}</p>}
      </CardContent>
    </Card>
  )
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
})
