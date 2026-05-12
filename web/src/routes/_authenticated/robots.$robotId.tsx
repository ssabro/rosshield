import { createFileRoute, Link } from '@tanstack/react-router'

import { useFleet, useRobot } from '@/api/hooks'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

import type { Robot } from '@/api/hooks'

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

      <p className="text-xs text-muted-foreground">
        <Link to="/robots" className="underline">
          {t('robots.detail.back')}
        </Link>
      </p>
    </div>
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

// silence unused import (Robot type — referenced via useRobot return).
const _typeRef: undefined | Robot = undefined
void _typeRef

export const Route = createFileRoute('/_authenticated/robots/$robotId')({
  component: RobotDetailPage,
})
