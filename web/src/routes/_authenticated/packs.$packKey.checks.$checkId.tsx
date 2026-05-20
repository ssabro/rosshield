import { createFileRoute, Link } from '@tanstack/react-router'

import { ApiError } from '@/api/errors'
import { useCheck, useCheckSelftest, usePack } from '@/api/hooks'
import { SeverityBadge } from '@/components/common/SeverityBadge'
import { Breadcrumbs } from '@/components/layout/Breadcrumbs'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { CardSkeleton } from '@/components/ui/skeleton'

// `/packs/{packKey}/checks/{checkId}` — 단일 check 상세 (E12 Stage 6).
//   - 기본 메타(id, severity, title, description)
//   - AuditCommand (bash one-liner — pack converter 직번역 결과)
//   - EvaluationRule (sealed AST, JSON pretty)
//   - Rationale + FixGuidance (운영자 친화적 설명)

function CheckDetailPage(): React.ReactElement {
  const { packKey, checkId } = Route.useParams()
  return <CheckDetailView packKey={packKey} checkId={checkId} />
}

// a11y-drilldown.test.tsx mount용 named export — Route.useParams 의존 분리.
export function CheckDetailView({ packKey, checkId }: { packKey: string; checkId: string }): React.ReactElement {
  const t = useT()
  const detailQuery = useCheck(packKey, checkId)
  const packQuery = usePack(packKey)

  if (detailQuery.isPending) {
    return (
      <div className="space-y-4">
        <Breadcrumbs
          items={[
            { label: t('nav.system'), to: '/system' },
            {
              label: packQuery.data?.name ?? packKey,
              to: '/packs/$packKey',
              params: { packKey },
            },
            { label: checkId },
          ]}
        />
        <PageHeader title={checkId} description={packKey} />
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
            {
              label: packQuery.data?.name ?? packKey,
              to: '/packs/$packKey',
              params: { packKey },
            },
            { label: checkId },
          ]}
        />
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
      <Breadcrumbs
        items={[
          { label: t('nav.system'), to: '/system' },
          {
            label: packQuery.data?.name ?? c.packKey,
            to: '/packs/$packKey',
            params: { packKey: c.packKey },
          },
          { label: c.checkId },
        ]}
      />
      <PageHeader
        title={c.title}
        description={c.checkId}
        badge={<SeverityBadge severity={c.severity} size="sm" />}
      />

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

      {c.auditCommand === 'true' && c.packKey === 'cis-ubuntu-2404' && (
        <Card className="border-yellow-500/40 bg-yellow-500/5">
          <CardHeader>
            <CardTitle className="text-sm text-yellow-700 dark:text-yellow-400">
              {t('checks.detail.degraded.title')}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <p className="text-foreground">{t('checks.detail.degraded.description')}</p>
            <p className="text-xs text-muted-foreground">
              {t('checks.detail.degraded.docsHint')}{' '}
              <code className="rounded bg-muted px-1 py-0.5 text-xs">
                docs/operations/cis-ubuntu-2404-degraded.md
              </code>
              {' '}— {t('checks.detail.degraded.searchHint', { checkId: c.checkId })}
            </p>
          </CardContent>
        </Card>
      )}

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

      <SelftestCard packKey={packKey} checkId={checkId} />
    </div>
  )
}

// SelftestCard — builtin pack 한정. 404 시 unsupported 안내, 데이터 시 케이스 리스트.
function SelftestCard({ packKey, checkId }: { packKey: string; checkId: string }): React.ReactElement {
  const t = useT()
  const q = useCheckSelftest(packKey, checkId)

  if (q.isPending) return <></>
  if (q.isError) {
    const status = q.error instanceof ApiError ? q.error.status : 0
    if (status === 404) {
      return (
        <Card>
          <CardHeader>
            <CardTitle>{t('checks.detail.selftest')}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">{t('checks.detail.selftest.unsupported')}</p>
          </CardContent>
        </Card>
      )
    }
    return (
      <Card>
        <CardHeader>
          <CardTitle>{t('checks.detail.selftest')}</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-destructive">{t('checks.detail.selftest.error')}</p>
        </CardContent>
      </Card>
    )
  }

  const cases = q.data?.cases ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {t('checks.detail.selftest')} ({cases.length})
        </CardTitle>
      </CardHeader>
      <CardContent>
        {cases.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('checks.detail.selftest.empty')}</p>
        ) : (
          <ul className="space-y-3">
            {cases.map((c, idx) => (
              <SelftestCaseRow key={idx} idx={idx} case_={c} />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

function SelftestCaseRow({
  idx,
  case_,
}: {
  idx: number
  case_: { name: string; input: { stdout?: string; stderr?: string; exitCode?: number }; expectedOutcome: string }
}): React.ReactElement {
  const outcomeVariant: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
    PASS: 'default',
    FAIL: 'destructive',
    INDETERMINATE: 'secondary',
    ERROR: 'destructive',
    SKIPPED: 'outline',
  }
  return (
    <li className="rounded-md border border-border bg-muted/30 p-3 text-xs">
      <div className="mb-2 flex items-center gap-2">
        <span className="font-medium">#{idx + 1}</span>
        <span className="text-muted-foreground">{case_.name}</span>
        <Badge variant={outcomeVariant[case_.expectedOutcome] ?? 'outline'}>
          → {case_.expectedOutcome}
        </Badge>
      </div>
      <div className="grid grid-cols-1 gap-1 sm:grid-cols-[6rem_1fr]">
        {case_.input.stdout !== undefined && (
          <>
            <span className="text-muted-foreground">stdout</span>
            <pre className="break-all font-mono">{JSON.stringify(case_.input.stdout)}</pre>
          </>
        )}
        {case_.input.stderr !== undefined && case_.input.stderr !== '' && (
          <>
            <span className="text-muted-foreground">stderr</span>
            <pre className="break-all font-mono">{JSON.stringify(case_.input.stderr)}</pre>
          </>
        )}
        {case_.input.exitCode !== undefined && (
          <>
            <span className="text-muted-foreground">exitCode</span>
            <pre className="font-mono">{case_.input.exitCode}</pre>
          </>
        )}
      </div>
    </li>
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

export const Route = createFileRoute('/_authenticated/packs/$packKey/checks/$checkId')({
  component: CheckDetailPage,
})
