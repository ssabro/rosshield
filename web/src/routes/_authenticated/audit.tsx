import { createFileRoute } from '@tanstack/react-router'

import { ApiError } from '@/api/errors'
import { useAuditHead } from '@/api/hooks'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

// `/audit` — 감사 체인의 현재 head를 표시 (B1).
//   - GET /api/v1/audit/head → ChainHead 메타.
//   - 외부 검증(verify)은 Phase 3 — 현재는 CLI(`rosshield report verify`).
function AuditPage(): React.ReactElement {
  const t = useT()
  const head = useAuditHead()

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.audit.title')}
        description={t('pages.audit.description')}
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium text-muted-foreground">
            Chain Head
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          {head.isPending && (
            <p className="text-muted-foreground">{t('common.loading')}</p>
          )}
          {head.isError && (
            <p className="text-destructive">
              {head.error instanceof ApiError
                ? head.error.message
                : t('audit.error.fallback')}
            </p>
          )}
          {head.isSuccess && (
            <>
              <Row label={t('audit.head.seq')} value={String(head.data.seq)} mono />
              <Row label={t('audit.head.hash')} value={head.data.hashHex} mono />
              {head.data.updatedAt && (
                <Row
                  label={t('audit.head.updated')}
                  value={new Date(head.data.updatedAt).toLocaleString()}
                />
              )}
              {head.data.seq === 0 && (
                <p className="rounded-md border border-dashed border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
                  {t('audit.head.empty')}
                </p>
              )}
            </>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium text-muted-foreground">
            {t('audit.verify.title')}
          </CardTitle>
          <CardDescription className="text-xs">
            {t('audit.verify.description')}
          </CardDescription>
        </CardHeader>
        <CardContent className="text-xs text-muted-foreground">
          <code className="rounded bg-muted px-1.5 py-0.5">
            rosshield report verify --bundle path/to/report.tar
          </code>
        </CardContent>
      </Card>
    </div>
  )
}

function Row({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}): React.ReactElement {
  return (
    <div className="grid grid-cols-1 gap-1 sm:grid-cols-[10rem_1fr]">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span
        className={
          mono
            ? 'break-all font-mono text-xs text-foreground'
            : 'text-xs text-foreground'
        }
      >
        {value}
      </span>
    </div>
  )
}

export const Route = createFileRoute('/_authenticated/audit')({
  component: AuditPage,
})
