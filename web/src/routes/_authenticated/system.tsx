import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'

import { ApiError } from '@/api/errors'
import { useLicenseInfo } from '@/api/hooks'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

import type { DictKey } from '@/i18n/dict'

// `/system` — B6+B7 운영 정보 dashboard.
//
//   - Service health (healthz fetch, public)
//   - HA status (healthz.ha 필드 — E25)
//   - License usage (useLicenseInfo, E24)
//   - Backups CLI 안내 (E28; 자동 백업·web 다운로드는 B7 후속)
//
// 백엔드 변경 0 — 이미 노출된 /healthz + GET /api/v1/license 만 사용.

interface HealthHA {
  enabled: boolean
  role: 'leader' | 'follower'
  epoch: number
  leaderId?: string
  lastHeartbeatAt?: string
}

interface HealthAudit {
  headSeq: number
  lastCheckpointSeq: number
  status: string
}

interface HealthComponents {
  storage: string
  eventbus: string
  scheduler: string
  signer: string
}

interface HealthResponse {
  status: string
  components: HealthComponents
  audit: HealthAudit
  ha?: HealthHA
}

function useHealthz() {
  return useQuery({
    queryKey: ['healthz'],
    queryFn: async (): Promise<HealthResponse> => {
      const r = await fetch('/healthz', { credentials: 'include' })
      if (!r.ok && r.status !== 503) {
        throw new ApiError(r.status, `healthz: ${r.statusText}`)
      }
      return (await r.json()) as HealthResponse
    },
    refetchInterval: 10_000,
  })
}

function SystemPage(): React.ReactElement {
  const t = useT()

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.system.title')}
        description={t('pages.system.description')}
      />

      <HealthCard />
      <HACard />
      <LicenseUsageCard />
      <BackupsCard />
    </div>
  )
}

function HealthCard(): React.ReactElement {
  const t = useT()
  const q = useHealthz()

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('system.health.section')}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        {q.isPending && <p className="text-muted-foreground">{t('common.loading')}</p>}
        {q.isError && (
          <p className="text-destructive">{t('system.health.error')}</p>
        )}
        {q.isSuccess && (
          <>
            <div className="flex items-center gap-2">
              <Badge
                variant={q.data.status === 'ok' ? 'default' : 'destructive'}
              >
                {q.data.status}
              </Badge>
            </div>
            <Row label={t('system.health.storage')} value={q.data.components.storage} />
            <Row label={t('system.health.eventbus')} value={q.data.components.eventbus} />
            <Row label={t('system.health.scheduler')} value={q.data.components.scheduler} />
            <Row label={t('system.health.signer')} value={q.data.components.signer} mono />
            <div className="pt-2">
              <p className="text-xs text-muted-foreground">{t('system.health.audit')}</p>
              <Row
                label={t('system.health.audit.head')}
                value={String(q.data.audit.headSeq)}
                mono
              />
              <Row
                label={t('system.health.audit.checkpoint')}
                value={String(q.data.audit.lastCheckpointSeq)}
                mono
              />
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}

function HACard(): React.ReactElement {
  const t = useT()
  const q = useHealthz()
  const ha = q.data?.ha

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('system.ha.section')}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        {q.isSuccess && !ha && (
          <p className="rounded-md border border-dashed border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
            {t('system.ha.disabled')}
          </p>
        )}
        {ha && (
          <>
            <div className="flex items-center gap-2">
              <Badge variant={ha.role === 'leader' ? 'default' : 'secondary'}>
                {t(haRoleLabelKey(ha.role))}
              </Badge>
            </div>
            <Row label={t('system.ha.epoch')} value={String(ha.epoch)} mono />
            <Row label={t('system.ha.leaderId')} value={ha.leaderId ?? '—'} mono />
            {ha.lastHeartbeatAt && (
              <Row
                label={t('system.ha.heartbeat')}
                value={new Date(ha.lastHeartbeatAt).toLocaleString()}
              />
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}

function LicenseUsageCard(): React.ReactElement {
  const t = useT()
  const q = useLicenseInfo()

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('settings.license.section')}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        {q.isPending && <p className="text-muted-foreground">{t('common.loading')}</p>}
        {q.isError && (
          <p className="text-destructive">
            {q.error instanceof ApiError ? q.error.message : t('settings.license.error')}
          </p>
        )}
        {q.isSuccess && (
          <>
            <div className="flex items-center gap-2">
              <Badge
                variant={q.data.edition === 'enterprise' ? 'default' : 'secondary'}
              >
                {q.data.edition}
              </Badge>
              {q.data.expired && (
                <Badge variant="destructive" className="text-[10px]">
                  {t('settings.license.expired')}
                </Badge>
              )}
            </div>
            <Row
              label={t('settings.license.quotas.robotsMax')}
              value={
                q.data.quotas.robotsMax > 0
                  ? String(q.data.quotas.robotsMax)
                  : t('settings.license.quotas.unlimited')
              }
              mono
            />
            <Row
              label={t('settings.license.quotas.scansPerDay')}
              value={
                q.data.quotas.scansPerDay > 0
                  ? String(q.data.quotas.scansPerDay)
                  : t('settings.license.quotas.unlimited')
              }
              mono
            />
            <Row
              label={t('settings.license.quotas.llmTokensPerDay')}
              value={
                q.data.quotas.llmTokensPerDay > 0
                  ? String(q.data.quotas.llmTokensPerDay)
                  : t('settings.license.quotas.unlimited')
              }
              mono
            />
          </>
        )}
      </CardContent>
    </Card>
  )
}

function BackupsCard(): React.ReactElement {
  const t = useT()

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('system.backups.section')}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <p className="text-muted-foreground">{t('system.backups.intro')}</p>
        <div className="space-y-1">
          <p className="text-xs text-muted-foreground">{t('system.backups.cli.label')}</p>
          <pre className="rounded-md bg-muted px-3 py-2 text-xs font-mono break-all">
            {t('system.backups.cli.example')}
          </pre>
        </div>
        <p className="rounded-md border border-dashed border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
          {t('system.backups.cron')}
        </p>
        <p className="text-xs text-muted-foreground">{t('system.backups.future')}</p>
      </CardContent>
    </Card>
  )
}

function haRoleLabelKey(role: 'leader' | 'follower'): DictKey {
  return role === 'leader' ? 'system.ha.role.leader' : 'system.ha.role.follower'
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
    <div className="grid grid-cols-1 gap-1 sm:grid-cols-[12rem_1fr]">
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

export const Route = createFileRoute('/_authenticated/system')({
  component: SystemPage,
})
