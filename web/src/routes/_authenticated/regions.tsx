import { createFileRoute } from '@tanstack/react-router'

import { Globe, ServerOff } from 'lucide-react'

import { ApiError } from '@/api/errors'
import {
  useAuditChainHeadSHA,
  useFailoverHistory,
  useReplicas,
} from '@/api/hooks'

import type { AuditChainHeadSHA, FailoverEvent } from '@/api/hooks'
import { AuditConsistencyCard } from '@/components/regions/AuditConsistencyCard'
import { RegionHealthCard } from '@/components/regions/RegionHealthCard'
import { RegionTimelineCard } from '@/components/regions/RegionTimelineCard'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useT } from '@/i18n/t'
import { requirePermission } from '@/lib/route-guards'

// `/regions` — Phase 10.A-2/3/4 multi-region UI 표면화.
//
// Phase 8(PG cross-region replication) + Phase 9(Patroni 자동 failover) + Phase 10
// (운영자 가시성 표면화) 인프라를 한곳에서 노출. backend는 admin 전용
// (handlers.go: ResourceTenantAdmin/ActionAdmin) — beforeLoad guard도 동일 매트릭스.
//
// 본 round 산출:
//   - self-region badge + selfRole (page header)
//   - replicas 목록 RegionHealthCard grid (10.A-2)
//   - AuditConsistencyCard — audit chain head sha 정합 (10.A-3)
//   - RegionTimelineCard — region cutover 이력 timeline (10.A-4)
//
// 후속 (10.A-5+): Prometheus alert rule + webhook trigger + e2e.

export function RegionsPage(): React.ReactElement {
  const t = useT()
  const q = useReplicas()
  const auditQ = useAuditChainHeadSHA()
  const timelineQ = useFailoverHistory(10)

  const headerBadge = q.isSuccess ? (
    <div className="flex items-center gap-1.5">
      <Badge variant="secondary" className="text-[10px]">
        {t('regions.self.label')}: {q.data.selfRegion || '—'}
      </Badge>
      <Badge variant="outline" className="text-[10px] uppercase tracking-wide">
        {q.data.selfRole || '—'}
      </Badge>
    </div>
  ) : undefined

  const selfRegion = q.data?.selfRegion ?? ''

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('regions.title')}
        description={t('regions.description')}
        badge={headerBadge}
      />

      {q.isPending && <ReplicasSkeleton />}
      {q.isError && <ReplicasError error={q.error} />}
      {q.isSuccess && q.data.replicas.length === 0 && (
        <EmptyState
          icon={ServerOff}
          title={t('regions.empty')}
          description={t('regions.empty.description')}
          className="border-dashed"
        />
      )}
      {q.isSuccess && q.data.replicas.length > 0 && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {q.data.replicas.map((replica) => (
            <RegionHealthCard key={replica.region} replica={replica} />
          ))}
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <AuditConsistencySection
          isPending={auditQ.isPending}
          isError={auditQ.isError}
          error={auditQ.error}
          head={auditQ.data}
          selfRegion={selfRegion}
        />
        <RegionTimelineSection
          isPending={timelineQ.isPending}
          isError={timelineQ.isError}
          error={timelineQ.error}
          events={timelineQ.data?.failovers ?? []}
          updatedAt={timelineQ.dataUpdatedAt}
        />
      </div>
    </div>
  )
}

function AuditConsistencySection({
  isPending,
  isError,
  error,
  head,
  selfRegion,
}: {
  isPending: boolean
  isError: boolean
  error: unknown
  head: AuditChainHeadSHA | undefined
  selfRegion: string
}): React.ReactElement {
  const t = useT()
  if (isPending) {
    return (
      <Card>
        <CardContent
          className="space-y-3 p-6"
          role="status"
          aria-label="audit consistency loading"
        >
          <Skeleton className="h-5 w-1/3" />
          <Skeleton className="h-3 w-2/3" />
          <Skeleton className="h-3 w-1/2" />
        </CardContent>
      </Card>
    )
  }
  if (isError) {
    const msg =
      error instanceof ApiError ? error.message : t('regions.audit.error')
    return (
      <EmptyState
        icon={Globe}
        title={t('regions.audit.error')}
        description={msg}
        className="border-dashed border-destructive/40"
      />
    )
  }
  return <AuditConsistencyCard head={head} selfRegion={selfRegion} />
}

function RegionTimelineSection({
  isPending,
  isError,
  error,
  events,
  updatedAt,
}: {
  isPending: boolean
  isError: boolean
  error: unknown
  events: ReadonlyArray<FailoverEvent>
  updatedAt: number | undefined
}): React.ReactElement {
  const t = useT()
  if (isPending) {
    return (
      <Card>
        <CardContent
          className="space-y-3 p-6"
          role="status"
          aria-label="failover timeline loading"
        >
          <Skeleton className="h-5 w-1/3" />
          <Skeleton className="h-3 w-2/3" />
          <Skeleton className="h-3 w-1/2" />
        </CardContent>
      </Card>
    )
  }
  if (isError) {
    const msg =
      error instanceof ApiError ? error.message : t('regions.timeline.error')
    return (
      <EmptyState
        icon={Globe}
        title={t('regions.timeline.error')}
        description={msg}
        className="border-dashed border-destructive/40"
      />
    )
  }
  const updatedIso = updatedAt ? new Date(updatedAt).toISOString() : undefined
  return <RegionTimelineCard events={events} updatedAt={updatedIso} />
}

function ReplicasSkeleton(): React.ReactElement {
  return (
    <div
      className="grid grid-cols-1 gap-4 lg:grid-cols-2"
      role="status"
      aria-label="불러오는 중"
    >
      {Array.from({ length: 2 }).map((_, i) => (
        <Card key={i}>
          <CardContent className="space-y-3 p-6">
            <Skeleton className="h-5 w-1/3" />
            <Skeleton className="h-3 w-2/3" />
            <Skeleton className="h-3 w-1/2" />
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function ReplicasError({ error }: { error: unknown }): React.ReactElement {
  const t = useT()
  const msg =
    error instanceof ApiError ? error.message : t('regions.error')
  return (
    <EmptyState
      icon={Globe}
      title={t('regions.error')}
      description={msg}
      className="border-dashed border-destructive/40"
    />
  )
}

export const Route = createFileRoute('/_authenticated/regions')({
  beforeLoad: () => requirePermission('tenant_admin', 'admin'),
  component: RegionsPage,
})
