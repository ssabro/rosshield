import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'

import {
  useDeleteRobot,
  useFleet,
  useIsAdmin,
  useRobot,
  useRobotResults,
} from '@/api/hooks'
import { Breadcrumbs } from '@/components/layout/Breadcrumbs'
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

import type { Robot, RobotResult } from '@/api/hooks'

// `/robots/$robotId` — 단일 robot 상세 (모든 인증 사용자).
function RobotDetailPage(): React.ReactElement {
  const t = useT()
  const { robotId } = Route.useParams()
  const robotQuery = useRobot(robotId)
  const robot = robotQuery.data
  // fleetId는 robot fetch 후에만 알 수 있음 — useFleet은 enabled !!fleetId.
  const fleetQuery = useFleet(robot?.fleetId)

  if (robotQuery.isPending) {
    return (
      <div className="space-y-6">
        <PageHeader title={t('pages.robots.title')} />
        <p className="text-sm text-muted-foreground">{t('robots.detail.loading')}</p>
      </div>
    )
  }
  if (!robot || robotQuery.isError) {
    return (
      <div className="space-y-6">
        <PageHeader title={t('pages.robots.title')} />
        <Card>
          <CardContent className="py-6 text-sm text-destructive">
            {t('robots.detail.notFound')}{' '}
            <Link to="/robots" className="underline">
              {t('robots.detail.back')}
            </Link>
          </CardContent>
        </Card>
      </div>
    )
  }

  const tags = Array.isArray(robot.tags) ? (robot.tags as unknown[]) : []

  const role = typeof robot.role === 'string' ? robot.role : ''
  const osDistro = typeof robot.osDistro === 'string' ? robot.osDistro : ''
  const rosDistro = typeof robot.rosDistro === 'string' ? robot.rosDistro : ''

  return (
    <div className="space-y-6">
      <Breadcrumbs
        items={[
          { label: t('nav.fleets'), to: '/fleets' },
          {
            label: fleetQuery.data?.name ?? robot.fleetId,
            to: '/fleets/$fleetId',
            params: { fleetId: robot.fleetId },
          },
          { label: t('nav.robots'), to: '/robots' },
          { label: robot.name },
        ]}
      />
      <PageHeader title={robot.name} description={role} />

      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle>{t('robots.detail.metaTitle')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <MetaRow label={t('robots.detail.id')} value={<span className="font-mono">{robot.id}</span>} />
          <MetaRow
            label={t('robots.detail.fleet')}
            value={
              <Link
                to="/fleets/$fleetId"
                params={{ fleetId: robot.fleetId }}
                className="font-mono hover:underline"
              >
                {fleetQuery.data?.name ?? robot.fleetId}
              </Link>
            }
          />
          <MetaRow
            label={t('robots.detail.host')}
            value={
              <span className="font-mono">
                {robot.host}:{robot.port}
              </span>
            }
          />
          <MetaRow label={t('robots.detail.authType')} value={robot.authType} />
          <MetaRow
            label={t('robots.detail.criticality')}
            value={<Badge variant="secondary">{robot.criticality}</Badge>}
          />
          {osDistro && (
            <MetaRow
              label={t('robots.detail.osDistro')}
              value={<span className="font-mono">{osDistro}</span>}
            />
          )}
          {rosDistro && (
            <MetaRow
              label={t('robots.detail.rosDistro')}
              value={<span className="font-mono">{rosDistro}</span>}
            />
          )}
          <MetaRow
            label={t('robots.detail.tags')}
            value={
              tags.length === 0 ? (
                <span className="text-xs text-muted-foreground">-</span>
              ) : (
                <div className="flex flex-wrap gap-1">
                  {tags.map((tag, i) => (
                    <Badge key={i} variant="outline" className="text-xs">
                      {String(tag)}
                    </Badge>
                  ))}
                </div>
              )
            }
          />
        </CardContent>
      </Card>

      <RobotResultsCard robotId={robot.id} />

      <DeleteRobotCard robot={robot} />

      <p className="text-xs text-muted-foreground">
        <Link to="/robots" className="underline">
          {t('robots.detail.back')}
        </Link>
      </p>
    </div>
  )
}

// DeleteRobotCard — admin only, 2-step confirm. 성공 시 /robots로 navigate.
function DeleteRobotCard({ robot }: { robot: Robot }): React.ReactElement | null {
  const t = useT()
  const isAdmin = useIsAdmin()
  const navigate = useNavigate()
  const del = useDeleteRobot()
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState('')

  if (!isAdmin) return null

  if (confirming) {
    return (
      <Card className="max-w-xl border-destructive">
        <CardHeader>
          <CardTitle className="text-sm text-destructive">
            {t('robots.detail.delete.confirmTitle')}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <p>{t('robots.detail.delete.confirmBody')}</p>
          <div className="flex items-center gap-2">
            <Button
              variant="destructive"
              size="sm"
              disabled={del.isPending}
              onClick={() =>
                del.mutate(robot.id, {
                  onSuccess: () => {
                    void navigate({ to: '/robots', replace: true })
                  },
                  onError: (e) =>
                    setError(e instanceof Error ? e.message : t('robots.detail.delete.error')),
                })
              }
            >
              {del.isPending
                ? t('robots.detail.delete.pending')
                : t('robots.detail.delete.yes')}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setConfirming(false)
                setError('')
              }}
            >
              {t('robots.detail.delete.cancel')}
            </Button>
            {error && <span className="text-xs text-destructive">{error}</span>}
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card className="max-w-xl">
      <CardHeader>
        <CardTitle className="text-sm">{t('robots.detail.delete.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        <Button variant="destructive" size="sm" onClick={() => setConfirming(true)}>
          {t('robots.detail.delete.button')}
        </Button>
      </CardContent>
    </Card>
  )
}

function MetaRow({
  label,
  value,
}: {
  label: string
  value: React.ReactNode
}): React.ReactElement {
  return (
    <div className="flex items-baseline gap-2">
      <span className="w-32 shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 flex-1">{value}</span>
    </div>
  )
}

// RobotResultsCard — useRobotResults hook으로 최근 진단 결과 20개 표시.
function RobotResultsCard({ robotId }: { robotId: string }): React.ReactElement {
  const t = useT()
  const q = useRobotResults(robotId, 20)
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('robots.detail.results.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        {q.isPending ? (
          <p className="text-sm text-muted-foreground">
            {t('robots.detail.results.loading')}
          </p>
        ) : q.isError ? (
          <p className="text-sm text-destructive">
            {q.error instanceof Error
              ? q.error.message
              : t('robots.detail.results.error')}
          </p>
        ) : (q.data?.length ?? 0) === 0 ? (
          <p className="text-sm text-muted-foreground">
            {t('robots.detail.results.empty')}
          </p>
        ) : (
          <div className="space-y-1">
            {q.data?.map((r) => <ResultRow key={r.id} result={r} />)}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function ResultRow({ result }: { result: RobotResult }): React.ReactElement {
  return (
    <div className="flex items-center justify-between rounded border border-border px-3 py-2 text-sm">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <Badge variant={outcomeVariant(result.outcome)}>{result.outcome}</Badge>
          <span className="font-mono text-xs">{result.checkId}</span>
        </div>
        {result.evalReason && (
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            {result.evalReason}
          </p>
        )}
      </div>
      <div className="ml-4 shrink-0 text-xs text-muted-foreground">
        <div className="text-right">{formatRelative(result.executedAt)}</div>
        <div className="text-right">{result.durationMs}ms</div>
      </div>
    </div>
  )
}

function outcomeVariant(
  outcome: string,
): 'default' | 'destructive' | 'secondary' | 'outline' {
  switch (outcome) {
    case 'pass':
      return 'default'
    case 'fail':
    case 'error':
      return 'destructive'
    case 'indeterminate':
      return 'secondary'
    default:
      return 'outline'
  }
}

function formatRelative(iso?: string): string {
  if (!iso) return ''
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  const sec = Math.round((Date.now() - t) / 1000)
  if (sec < 60) return `${sec}s`
  const min = Math.round(sec / 60)
  if (min < 60) return `${min}m`
  const hr = Math.round(min / 60)
  if (hr < 24) return `${hr}h`
  const day = Math.round(hr / 24)
  return `${day}d`
}

// silence unused import (Robot type — referenced via useRobot return).
const _typeRef: undefined | Robot = undefined
void _typeRef

export const Route = createFileRoute('/_authenticated/robots/$robotId')({
  component: RobotDetailPage,
})
