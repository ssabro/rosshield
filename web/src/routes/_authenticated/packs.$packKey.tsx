import { createFileRoute, Link } from '@tanstack/react-router'

import { ApiError } from '@/api/errors'
import { usePack } from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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

function PackDetailPage(): React.ReactElement {
  const { packKey } = Route.useParams()
  const t = useT()
  const detailQuery = usePack(packKey)

  if (detailQuery.isPending) {
    return (
      <div className="space-y-4">
        <PageHeader title={t('packs.detail.title')} description={packKey} />
        <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
      </div>
    )
  }

  if (detailQuery.isError) {
    const status = detailQuery.error instanceof ApiError ? detailQuery.error.status : 0
    return (
      <div className="space-y-4">
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

  return (
    <div className="space-y-4">
      <PageHeader title={pack.name} description={pack.description ?? pack.packKey} />

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

      <Card>
        <CardHeader>
          <CardTitle>{t('packs.detail.checks')}</CardTitle>
        </CardHeader>
        <CardContent>
          {pack.checks.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t('packs.detail.checks.empty')}</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('packs.detail.col.checkId')}</TableHead>
                  <TableHead>{t('packs.detail.col.severity')}</TableHead>
                  <TableHead>{t('packs.detail.col.title')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pack.checks.map((c) => (
                  <CheckRow key={c.id} check={c} />
                ))}
              </TableBody>
            </Table>
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

function CheckRow({ check }: { check: PackCheck }): React.ReactElement {
  return (
    <TableRow>
      <TableCell className="font-mono text-xs">{check.checkId}</TableCell>
      <TableCell>
        <SeverityBadge severity={check.severity} />
      </TableCell>
      <TableCell className="text-xs">{check.title}</TableCell>
    </TableRow>
  )
}

function SeverityBadge({
  severity,
}: {
  severity: 'low' | 'medium' | 'high' | 'critical'
}): React.ReactElement {
  const variant: Record<typeof severity, 'default' | 'secondary' | 'destructive' | 'outline'> = {
    low: 'outline',
    medium: 'secondary',
    high: 'default',
    critical: 'destructive',
  }
  return <Badge variant={variant[severity]}>{severity}</Badge>
}

export const Route = createFileRoute('/_authenticated/packs/$packKey')({
  component: PackDetailPage,
})
