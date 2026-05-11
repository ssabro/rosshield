import { createFileRoute, Link } from '@tanstack/react-router'

import { ApiError } from '@/api/errors'
import { useCheck } from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

// `/packs/{packKey}/checks/{checkId}` — 단일 check 상세 (E12 Stage 6).
//   - 기본 메타(id, severity, title, description)
//   - AuditCommand (bash one-liner — pack converter 직번역 결과)
//   - EvaluationRule (sealed AST, JSON pretty)
//   - Rationale + FixGuidance (운영자 친화적 설명)

function CheckDetailPage(): React.ReactElement {
  const { packKey, checkId } = Route.useParams()
  const t = useT()
  const detailQuery = useCheck(packKey, checkId)

  if (detailQuery.isPending) {
    return (
      <div className="space-y-4">
        <PageHeader title={checkId} description={packKey} />
        <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
      </div>
    )
  }

  if (detailQuery.isError) {
    const status = detailQuery.error instanceof ApiError ? detailQuery.error.status : 0
    return (
      <div className="space-y-4">
        <PageHeader title={checkId} description={packKey} />
        <EmptyState
          title={status === 404 ? t('checks.detail.notFound') : t('checks.detail.error')}
          description={
            detailQuery.error instanceof Error
              ? detailQuery.error.message
              : t('checks.detail.error')
          }
          action={
            <Link
              to="/packs/$packKey"
              params={{ packKey }}
              className="text-sm underline"
            >
              {t('checks.detail.backToPack')}
            </Link>
          }
        />
      </div>
    )
  }

  const c = detailQuery.data!
  const evalRulePretty = JSON.stringify(c.evaluationRule, null, 2)

  return (
    <div className="space-y-4">
      <PageHeader title={c.title} description={c.checkId} />

      <Card>
        <CardHeader>
          <CardTitle>{t('checks.detail.meta')}</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 gap-2 sm:grid-cols-[10rem_1fr]">
            <Meta label={t('checks.detail.field.checkId')} value={c.checkId} mono />
            <Meta label={t('checks.detail.field.id')} value={c.id} mono />
            <Meta label={t('checks.detail.field.packKey')} value={c.packKey} mono>
              <Link
                to="/packs/$packKey"
                params={{ packKey: c.packKey }}
                className="ml-2 text-xs underline"
              >
                {t('checks.detail.viewPack')}
              </Link>
            </Meta>
            <dt className="text-xs text-muted-foreground">{t('checks.detail.field.severity')}</dt>
            <dd>
              <SeverityBadge severity={c.severity} />
            </dd>
            {c.description && (
              <Meta label={t('checks.detail.field.description')} value={c.description} />
            )}
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('checks.detail.auditCommand')}</CardTitle>
        </CardHeader>
        <CardContent>
          <pre className="overflow-x-auto rounded-md bg-muted/40 p-3 text-xs font-mono whitespace-pre-wrap">
            {c.auditCommand}
          </pre>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('checks.detail.evaluationRule')}</CardTitle>
        </CardHeader>
        <CardContent>
          <pre className="overflow-x-auto rounded-md bg-muted/40 p-3 text-xs font-mono">
            {evalRulePretty}
          </pre>
        </CardContent>
      </Card>

      {c.rationale && (
        <Card>
          <CardHeader>
            <CardTitle>{t('checks.detail.rationale')}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="whitespace-pre-wrap text-sm text-foreground">{c.rationale}</p>
          </CardContent>
        </Card>
      )}

      {c.fixGuidance && (
        <Card>
          <CardHeader>
            <CardTitle>{t('checks.detail.fixGuidance')}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="whitespace-pre-wrap text-sm text-foreground">{c.fixGuidance}</p>
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function Meta({
  label,
  value,
  mono,
  children,
}: {
  label: string
  value: string
  mono?: boolean
  children?: React.ReactNode
}): React.ReactElement {
  return (
    <>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={mono ? 'break-all font-mono text-xs' : 'text-sm'}>
        {value}
        {children}
      </dd>
    </>
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

export const Route = createFileRoute('/_authenticated/packs/$packKey/checks/$checkId')({
  component: CheckDetailPage,
})
