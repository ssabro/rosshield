import { createFileRoute, Link } from '@tanstack/react-router'
import { useState } from 'react'

import { ApiError } from '@/api/errors'
import { usePack } from '@/api/hooks'
import { SeverityBadge } from '@/components/common/SeverityBadge'
import { Breadcrumbs } from '@/components/layout/Breadcrumbs'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { CardSkeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type { PackCheck } from '@/api/hooks'

// `/packs/{packKey}` — 단일 벤치마크 팩 상세 (E12 Stage 5).
//   - 메타: name·version·vendor·packKey·installedAt·built-in 표시
//   - checks: id, code, severity, title (CheckID 알파벳 정렬)
//   - 운영자가 어떤 검사가 포함되어 있는지, 어떤 문제 영역을 cover하는지 한눈에 확인.

// a11y-drilldown.test.tsx mount용 named export.
// Route.useParams 의존을 갖지만 mocked useParams가 packKey를 제공하므로 mount 가능.
export function PackDetailPage(): React.ReactElement {
  const { packKey } = Route.useParams()
  const t = useT()
  const detailQuery = usePack(packKey)
  const [severityFilter, setSeverityFilter] = useState<string>('')

  if (detailQuery.isPending) {
    return (
      <div className="space-y-4">
        <Breadcrumbs
          items={[
            { label: t('nav.system'), to: '/system' },
            { label: packKey },
          ]}
        />
        <PageHeader title={t('packs.detail.title')} description={packKey} />
        <CardSkeleton />
        <CardSkeleton />
      </div>
    )
  }

  if (detailQuery.isError) {
    const status = detailQuery.error instanceof ApiError ? detailQuery.error.status : 0
    return (
      <div className="space-y-4">
        <Breadcrumbs
          items={[
            { label: t('nav.system'), to: '/system' },
            { label: packKey },
          ]}
        />
        <PageHeader title={t('packs.detail.title')} description={packKey} />
        <EmptyState
          title={status === 404 ? t('packs.detail.notFound') : t('packs.detail.error')}
          description={
            detailQuery.error instanceof Error
              ? detailQuery.error.message
              : t('packs.detail.error')
          }
          action={
            <Link to="/system" className="text-sm underline">
              {t('packs.detail.backToSystem')}
            </Link>
          }
        />
      </div>
    )
  }

  const pack = detailQuery.data!

  // PageHeader 우측 badge: version + check count 한 줄로 압축 표시.
  // built-in 팩은 secondary, tenant 팩은 outline 으로 시각 구분(scope에 따라).
  const headerBadge = (
    <span className="flex items-center gap-1">
      <Badge variant="outline" className="font-mono text-[10px]">
        v{pack.version}
      </Badge>
      <Badge variant="secondary" className="text-[10px]">
        {t('packs.detail.header.checkCount', {
          count: String(pack.checks.length),
        })}
      </Badge>
      <Badge
        variant={pack.isBuiltin ? 'default' : 'outline'}
        className="text-[10px]"
      >
        {pack.isBuiltin ? t('packs.scope.builtin') : t('packs.scope.tenant')}
      </Badge>
    </span>
  )

  return (
    <div className="space-y-4">
      <Breadcrumbs
        items={[
          { label: t('nav.system'), to: '/system' },
          { label: pack.name },
        ]}
      />
      <PageHeader
        title={pack.name}
        description={pack.description ?? pack.packKey}
        badge={headerBadge}
      />

      <Card>
        <CardHeader>
          <CardTitle>{t('packs.detail.meta')}</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 gap-2 sm:grid-cols-[10rem_1fr]">
            <Meta label={t('packs.detail.field.vendor')} value={pack.vendor} />
            <Meta label={t('packs.detail.field.version')} value={pack.version} />
            <Meta label={t('packs.detail.field.packKey')} value={pack.packKey} mono />
            <Meta label={t('packs.detail.field.installedAt')} value={new Date(pack.installedAt).toLocaleString()} />
            <Meta
              label={t('packs.detail.field.scope')}
              value={pack.isBuiltin ? t('packs.detail.scope.builtin') : t('packs.detail.scope.tenant')}
            />
            {pack.signerKeyId && (
              <Meta label={t('packs.detail.field.signerKeyId')} value={pack.signerKeyId} mono />
            )}
            <Meta label={t('packs.detail.field.checkCount')} value={String(pack.checks.length)} />
          </dl>
        </CardContent>
      </Card>

      {pack.checks.length > 0 && (
        <SeverityStats
          checks={pack.checks}
          active={severityFilter}
          onClick={(s) => setSeverityFilter(s)}
        />
      )}

      <Card>
        <CardHeader>
          <CardTitle>{t('packs.detail.checks')}</CardTitle>
        </CardHeader>
        <CardContent>
          {pack.checks.length === 0 ? (
            <EmptyState
              title={t('packs.detail.checks.empty')}
              size="sm"
            />
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('packs.detail.col.checkId')}</TableHead>
                    <TableHead>{t('packs.detail.col.severity')}</TableHead>
                    <TableHead>{t('packs.detail.col.title')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filterChecksBySeverity(pack.checks, severityFilter).map((c) => (
                    <CheckRow key={c.id} check={c} packKey={pack.packKey} />
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function Meta({ label, value, mono }: { label: string; value: string; mono?: boolean }): React.ReactElement {
  return (
    <>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={mono ? 'break-all font-mono text-xs' : 'text-sm'}>{value}</dd>
    </>
  )
}

function CheckRow({ check, packKey }: { check: PackCheck; packKey: string }): React.ReactElement {
  return (
    <TableRow>
      <TableCell className="font-mono text-xs">
        <Link
          to="/packs/$packKey/checks/$checkId"
          params={{ packKey, checkId: check.checkId }}
          className="hover:underline"
        >
          {check.checkId}
        </Link>
      </TableCell>
      <TableCell>
        <SeverityBadge severity={check.severity} />
      </TableCell>
      <TableCell className="text-xs">{check.title}</TableCell>
    </TableRow>
  )
}

// SeverityStats — pack의 checks를 severity별 카운트 카드 4개로 요약 + 클릭으로 필터.
//
// findings.tsx의 패턴 재사용. CIS pack은 critical/high/medium/low 4-tier(info 부재).
// 클릭 시 active 카드는 ring 강조. 같은 카드 재클릭은 필터 해제.
function SeverityStats({
  checks,
  active,
  onClick,
}: {
  checks: PackCheck[]
  active: string
  onClick: (severity: string) => void
}): React.ReactElement {
  const t = useT()
  const counts: Record<string, number> = { critical: 0, high: 0, medium: 0, low: 0 }
  for (const c of checks) {
    if (counts[c.severity] !== undefined) {
      counts[c.severity]!++
    }
  }
  const order: Array<{ severity: string; bg: string; text: string }> = [
    { severity: 'critical', bg: 'bg-destructive/10', text: 'text-destructive' },
    { severity: 'high', bg: 'bg-destructive/10', text: 'text-destructive' },
    { severity: 'medium', bg: 'bg-primary/10', text: 'text-primary' },
    { severity: 'low', bg: 'bg-muted', text: 'text-muted-foreground' },
  ]
  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
      {order.map((o) => {
        const isActive = active === o.severity
        const count = counts[o.severity] ?? 0
        return (
          <button
            key={o.severity}
            type="button"
            onClick={() => onClick(isActive ? '' : o.severity)}
            aria-pressed={isActive}
            aria-label={t('packs.detail.severityStats.toggle', {
              severity: o.severity,
              count: count.toString(),
            })}
            className={`flex flex-col items-start gap-1 rounded-md border px-3 py-2 text-left transition-all hover:border-foreground/40 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring ${o.bg} ${
              isActive ? 'ring-2 ring-foreground/40' : ''
            }`}
          >
            <span className={`text-xs font-medium ${o.text}`}>{t(`severity.${o.severity}` as 'severity.critical')}</span>
            <span className="text-2xl font-bold leading-none">{count}</span>
          </button>
        )
      })}
    </div>
  )
}

// filterChecksBySeverity — active severity 필터 적용. 빈 string이면 전체.
// 단위 테스트(packs.$packKey.test.tsx) 대상으로 export.
export function filterChecksBySeverity(
  checks: PackCheck[],
  severity: string,
): PackCheck[] {
  if (!severity) return checks
  return checks.filter((c) => c.severity === severity)
}

export const Route = createFileRoute('/_authenticated/packs/$packKey')({
  component: PackDetailPage,
})
