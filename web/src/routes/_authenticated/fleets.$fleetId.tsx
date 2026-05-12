import { createFileRoute, Link } from '@tanstack/react-router'

import { useFleet, useRobots } from '@/api/hooks'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { requireRole } from '@/lib/route-guards'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

import type { Robot } from '@/api/hooks'

// `/fleets/$fleetId` — 단일 fleet 메타 + 그 fleet의 robot 목록 (admin/auditor).
function FleetDetailPage(): React.ReactElement {
  const t = useT()
  const { fleetId } = Route.useParams()
  const fleetQuery = useFleet(fleetId)
  const robotsQuery = useRobots(fleetId)

  const fleet = fleetQuery.data

  if (fleetQuery.isPending) {
    return (
      <div className="space-y-6">
        <PageHeader title={t('pages.fleets.title')} />
        <p className="text-sm text-muted-foreground">{t('fleets.list.loading')}</p>
      </div>
    )
  }
  if (!fleet || fleetQuery.isError) {
    return (
      <div className="space-y-6">
        <PageHeader title={t('pages.fleets.title')} />
        <Card>
          <CardContent className="py-6 text-sm text-destructive">
            {t('fleets.detail.notFound')}{' '}
            <Link to="/fleets" className="underline">
              {t('fleets.detail.back')}
            </Link>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={fleet.name}
        description={fleet.description || t('fleets.detail.noDescription')}
      />

      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle>{t('fleets.detail.metaTitle')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <MetaRow label={t('fleets.detail.id')} value={<span className="font-mono">{fleet.id}</span>} />
          <MetaRow label={t('fleets.detail.tenant')} value={<span className="font-mono">{fleet.tenantId}</span>} />
          <MetaRow
            label={t('fleets.detail.robotCount')}
            value={<Badge variant="secondary">{fleet.robotCount}</Badge>}
          />
          {fleet.createdAt && (
            <MetaRow label={t('fleets.detail.createdAt')} value={fleet.createdAt} />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('fleets.detail.robotsTitle')}</CardTitle>
        </CardHeader>
        <CardContent>
          {robotsQuery.isPending ? (
            <p className="text-sm text-muted-foreground">{t('fleets.detail.robotsLoading')}</p>
          ) : robotsQuery.isError ? (
            <p className="text-sm text-destructive">
              {robotsQuery.error instanceof Error
                ? robotsQuery.error.message
                : t('fleets.detail.robotsError')}
            </p>
          ) : (robotsQuery.data?.length ?? 0) === 0 ? (
            <p className="text-sm text-muted-foreground">{t('fleets.detail.robotsEmpty')}</p>
          ) : (
            <div className="space-y-1">
              {robotsQuery.data?.map((r) => <RobotRow key={r.id} robot={r} />)}
            </div>
          )}
        </CardContent>
      </Card>

      <p className="text-xs text-muted-foreground">
        <Link to="/fleets" className="underline">
          {t('fleets.detail.back')}
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

function RobotRow({ robot }: { robot: Robot }): React.ReactElement {
  return (
    <div className="flex items-center justify-between rounded border border-border px-3 py-2 text-sm">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-medium">{robot.name}</span>
          <span className="font-mono text-xs text-muted-foreground">{robot.id}</span>
          <Badge variant="outline" className="text-xs">
            {robot.criticality}
          </Badge>
        </div>
        <p className="mt-0.5 text-xs text-muted-foreground">
          <span className="font-mono">
            {robot.host}:{robot.port}
          </span>{' '}
          · {robot.authType}
        </p>
      </div>
    </div>
  )
}

export const Route = createFileRoute('/_authenticated/fleets/$fleetId')({
  component: FleetDetailPage,
  beforeLoad: () => requireRole('admin', 'auditor'),
})
