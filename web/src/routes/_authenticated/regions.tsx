import { createFileRoute } from '@tanstack/react-router'

import { Globe, ServerOff } from 'lucide-react'

import { ApiError } from '@/api/errors'
import { useReplicas } from '@/api/hooks'
import { RegionHealthCard } from '@/components/regions/RegionHealthCard'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useT } from '@/i18n/t'
import { requirePermission } from '@/lib/route-guards'

// `/regions` — Phase 10.A-2 multi-region UI 표면화 1단계.
//
// Phase 8(PG cross-region replication) + Phase 9(Patroni 자동 failover) 인프라의
// 운영자 가시성 표면화. backend는 admin 전용(handlers.go: ResourceTenantAdmin/
// ActionAdmin) — beforeLoad guard도 동일 매트릭스 사용.
//
// 본 round 산출:
//   - self-region badge + selfRole (page header)
//   - replicas 목록 RegionHealthCard grid
//
// 후속 (10.A-3+): cross-region audit chain 비교 + lag alert + failover trigger UI.

export function RegionsPage(): React.ReactElement {
  const t = useT()
  const q = useReplicas()

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
    </div>
  )
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
