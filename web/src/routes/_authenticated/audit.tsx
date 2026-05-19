import { createFileRoute } from '@tanstack/react-router'

import { CheckCircle2 } from 'lucide-react'

import { ApiError } from '@/api/errors'
import { useAuditHead } from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { TextSkeleton } from '@/components/ui/skeleton'

// `/audit` — 감사 체인의 현재 head를 표시 (B1).
//   - GET /api/v1/audit/head → ChainHead 메타.
//   - 외부 검증(verify)은 Phase 3 — 현재는 CLI(`rosshield report verify`).
function AuditPage(): React.ReactElement {
  const t = useT()
  const head = useAuditHead()

  // PageHeader 우측 badge: chain head seq를 한눈에. 0이면 "genesis" 안내.
  const headerBadge = head.isSuccess ? (
    <Badge variant="secondary" className="font-mono text-[10px]">
      {head.data.seq === 0
        ? t('audit.header.badge.genesis')
        : t('audit.header.badge.seq', { seq: String(head.data.seq) })}
    </Badge>
  ) : undefined

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.audit.title')}
        description={t('pages.audit.description')}
        badge={headerBadge}
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium text-muted-foreground">
            Chain Head
          </CardTitle>
          <CardDescription className="text-xs">
            {t('audit.head.description')}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          {head.isPending && (
            <div className="space-y-2" aria-label={t('common.loading')}>
              <TextSkeleton className="w-1/3" />
              <TextSkeleton className="w-2/3" />
              <TextSkeleton className="w-1/2" />
            </div>
          )}
          {head.isError && (
            <p className="text-destructive">
              {head.error instanceof ApiError
                ? head.error.message
                : t('audit.error.fallback')}
            </p>
          )}
          {head.isSuccess && head.data.seq > 0 && (
            <>
              <Row label={t('audit.head.seq')} value={String(head.data.seq)} mono />
              <Row label={t('audit.head.hash')} value={head.data.hashHex} mono />
              {head.data.updatedAt && (
                <Row
                  label={t('audit.head.updated')}
                  value={new Date(head.data.updatedAt).toLocaleString()}
                />
              )}
              <div className="flex items-start gap-2 rounded-md border border-emerald-500/40 bg-emerald-500/5 px-3 py-2 text-xs text-emerald-700 dark:text-emerald-400">
                <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
                <span>
                  {t('audit.chain.healthy', {
                    seq: String(head.data.seq),
                  })}
                </span>
              </div>
            </>
          )}
          {head.isSuccess && head.data.seq === 0 && (
            <EmptyState
              title={t('audit.head.empty.title')}
              description={t('audit.head.empty')}
              size="sm"
            />
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
          <code className="block overflow-x-auto rounded bg-muted px-2 py-1.5 font-mono">
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
