import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'

import { ApiError } from '@/api/errors'
import { API_BASE_PATH } from '@/api/client'
import { useBackups, useIsAdminOrAuditor, useLicenseInfo, usePacks, useScans, useUsageStats } from '@/api/hooks'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { requireRole } from '@/lib/route-guards'
import { formatBytes } from '@/lib/utils'
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
      <UsageStatsCard />
      <ScansSeverityCard />
      <PacksCard />
      <BackupsCard />
    </div>
  )
}

// PacksCard — built-in + tenant 벤치마크 팩 표시 (E12 Stage 3).
//
// 운영자가 어떤 pack이 install되어 있는지 한눈에 확인. /scans 페이지의 Pack 선택
// 드롭다운은 같은 데이터를 사용 — 본 카드는 운영 시각화 용도.
function PacksCard(): React.ReactElement {
  const t = useT()
  const packsQuery = usePacks()
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('system.packs.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        {packsQuery.isPending ? (
          <p className="text-sm text-muted-foreground">{t('common.loading')}</p>
        ) : packsQuery.isError ? (
          <p className="text-sm text-destructive">
            {packsQuery.error instanceof Error
              ? packsQuery.error.message
              : t('system.packs.error')}
          </p>
        ) : (packsQuery.data?.length ?? 0) === 0 ? (
          <p className="text-sm text-muted-foreground">{t('system.packs.empty')}</p>
        ) : (
          <ul className="space-y-2">
            {packsQuery.data?.map((p) => (
              <li
                key={p.id}
                className="flex flex-col gap-2 rounded-md border border-border bg-muted/30 p-3 sm:flex-row sm:items-center sm:justify-between"
              >
                <div className="flex flex-col gap-1">
                  <div className="flex items-center gap-2">
                    <Link
                      to="/packs/$packKey"
                      params={{ packKey: p.packKey }}
                      className="font-medium text-foreground hover:underline"
                    >
                      {p.name}
                    </Link>
                    <Badge variant="outline">{p.version}</Badge>
                    <Badge variant={p.isBuiltin ? 'secondary' : 'outline'}>
                      {p.isBuiltin ? t('packs.scope.builtin') : t('packs.scope.tenant')}
                    </Badge>
                  </div>
                  <span className="text-xs text-muted-foreground">
                    {t('system.packs.vendor')}: {p.vendor} · {t('system.packs.installedAt')}:{' '}
                    {new Date(p.installedAt).toLocaleString()}
                  </span>
                  {p.description && (
                    <span className="text-xs text-muted-foreground">{p.description}</span>
                  )}
                </div>
                <span className="font-mono text-[11px] text-muted-foreground">
                  {p.packKey}
                </span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
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

function UsageStatsCard(): React.ReactElement {
  const t = useT()
  const q = useUsageStats()

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('system.usage.section')}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        {q.isPending && <p className="text-muted-foreground">{t('common.loading')}</p>}
        {q.isError && (
          <p className="text-destructive">
            {q.error instanceof ApiError ? q.error.message : t('system.usage.error')}
          </p>
        )}
        {q.isSuccess && (
          <>
            <p className="text-xs text-muted-foreground">{t('system.usage.processScopeNote')}</p>
            <Row label={t('system.usage.scansStarted')} value={String(q.data.scansStarted)} mono />
            <Row
              label={t('system.usage.scansCompleted')}
              value={`${q.data.scansCompletedSum} (completed: ${q.data.scansCompleted.completed ?? 0}, failed: ${q.data.scansCompleted.failed ?? 0}, cancelled: ${q.data.scansCompleted.cancelled ?? 0})`}
              mono
            />
            <Row label={t('system.usage.violations')} value={String(q.data.scanFailedChecks)} mono />
            {q.data.scansCompletedSum > 0 && (
              <Row
                label={t('system.usage.violationRate')}
                value={
                  (q.data.scanFailedChecks / q.data.scansCompletedSum).toFixed(2) +
                  ' / scan'
                }
                mono
              />
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}

// ScansSeverityCard — 최근 N=50 terminal 세션의 severity별 fail 카운트 합산.
//
// B 후속 — 운영자가 system dashboard에서 tenant 전체 누적 severity 분포를 한 곳에서
// 확인. 백엔드 endpoint 추가 0 — 기존 useScans 활용한 client-side aggregation. 합산 대상은
// terminal session(completed/failed/cancelled)만 — pending/running은 모두 0이라 noise.
// 카드형 표시는 packs.SeverityStats / SessionSeverityCardGrid 패턴 일관(D26-4).
const SCANS_SEVERITY_LIMIT = 50

function ScansSeverityCard(): React.ReactElement {
  const t = useT()
  const q = useScans({ limit: SCANS_SEVERITY_LIMIT })

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('system.scansSeverity.section')}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        {q.isPending && <p className="text-muted-foreground">{t('common.loading')}</p>}
        {q.isError && (
          <p className="text-destructive">
            {q.error instanceof ApiError ? q.error.message : t('system.scansSeverity.error')}
          </p>
        )}
        {q.isSuccess && (() => {
          const terminal = (q.data ?? []).filter(
            (s) => s.status === 'completed' || s.status === 'failed' || s.status === 'cancelled',
          )
          const totals = {
            critical: 0,
            high: 0,
            medium: 0,
            low: 0,
          }
          for (const s of terminal) {
            totals.critical += s.severityCriticalFailed
            totals.high += s.severityHighFailed
            totals.medium += s.severityMediumFailed
            totals.low += s.severityLowFailed
          }
          const totalFails = totals.critical + totals.high + totals.medium + totals.low
          const order: Array<{ severity: 'critical' | 'high' | 'medium' | 'low'; bg: string; text: string }> = [
            { severity: 'critical', bg: 'bg-destructive/10', text: 'text-destructive' },
            { severity: 'high', bg: 'bg-destructive/10', text: 'text-destructive' },
            { severity: 'medium', bg: 'bg-primary/10', text: 'text-primary' },
            { severity: 'low', bg: 'bg-muted', text: 'text-muted-foreground' },
          ]
          return (
            <>
              <p className="text-xs text-muted-foreground">
                {t('system.scansSeverity.scopeNote', {
                  count: terminal.length.toString(),
                  limit: SCANS_SEVERITY_LIMIT.toString(),
                })}
              </p>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                {order.map((o) => (
                  <div
                    key={o.severity}
                    className={`flex flex-col items-start gap-0.5 rounded-md border px-2.5 py-1.5 ${o.bg}`}
                  >
                    <span className={`text-[10px] font-medium uppercase ${o.text}`}>
                      {o.severity}
                    </span>
                    <span className="text-xl font-bold leading-none tabular-nums">
                      {totals[o.severity]}
                    </span>
                  </div>
                ))}
              </div>
              {totalFails === 0 && terminal.length > 0 && (
                <p className="text-xs text-muted-foreground">
                  {t('system.scansSeverity.allClean')}
                </p>
              )}
            </>
          )
        })()}
      </CardContent>
    </Card>
  )
}

function BackupsCard(): React.ReactElement {
  const t = useT()
  const q = useBackups()
  const canDownload = useIsAdminOrAuditor()
  // 최근 5개만 표시 (생성 시각 desc) — 더 많이 보고 싶으면 Stage 2-C에서 페이지네이션 추가.
  const recent = q.data
    ? [...q.data]
        .sort((a, b) => b.generatedAt.localeCompare(a.generatedAt))
        .slice(0, 5)
    : []

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('system.backups.section')}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <p className="text-muted-foreground">{t('system.backups.intro')}</p>

        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <p className="text-xs font-medium text-muted-foreground">
              {t('system.backups.list.section')}
            </p>
            {q.isFetching && q.isSuccess && (
              <span className="text-[10px] text-muted-foreground">
                {t('system.backups.refreshing')}
              </span>
            )}
          </div>

          {q.isPending && (
            <p className="text-xs text-muted-foreground">
              {t('system.backups.list.loading')}
            </p>
          )}
          {q.isError && (
            <p className="text-xs text-destructive">
              {q.error instanceof ApiError
                ? q.error.message
                : t('system.backups.list.error')}
            </p>
          )}
          {q.isSuccess && recent.length === 0 && (
            <p className="rounded-md border border-dashed border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
              {t('system.backups.list.empty')}
            </p>
          )}
          {q.isSuccess && recent.length > 0 && (
            <ul className="divide-y divide-border rounded-md border border-border">
              {recent.map((b) => (
                <BackupRow
                  key={b.filename}
                  backup={b}
                  canDownload={canDownload}
                />
              ))}
            </ul>
          )}
        </div>

        <div className="space-y-1 border-t border-border pt-3">
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

// BackupRow — 단일 백업 메타 + 다운로드 버튼.
//   다운로드: Stage 2-A `/api/v1/backups/{filename}/download` (Content-Disposition: attachment).
//   동일 origin + cookie 인증이라 별 Authorization 헤더 불필요 (anchor download 속성 사용).
//   RBAC Stage 2-B: admin·auditor만 활성. 그 외는 disabled span으로 렌더 + tooltip.
function BackupRow({
  backup,
  canDownload,
}: {
  backup: import('@/api/hooks').BackupMeta
  canDownload: boolean
}): React.ReactElement {
  const t = useT()
  const downloadUrl = `${API_BASE_PATH}/backups/${encodeURIComponent(backup.filename)}/download`
  const generated = (() => {
    const d = new Date(backup.generatedAt)
    return Number.isNaN(d.getTime()) ? backup.generatedAt : d.toLocaleString()
  })()
  const sha256Short = backup.sha256.slice(0, 16)

  return (
    <li className="flex flex-col gap-2 px-3 py-2 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex items-center gap-2">
          <span className="break-all font-mono text-xs text-foreground">
            {backup.filename}
          </span>
          <Badge variant={backup.includesEvidence ? 'default' : 'secondary'} className="text-[10px]">
            {backup.includesEvidence
              ? t('system.backups.evidence.included')
              : t('system.backups.evidence.metadata.only')}
          </Badge>
        </div>
        <div className="grid grid-cols-1 gap-x-4 gap-y-0.5 text-[11px] text-muted-foreground sm:grid-cols-3">
          <span>
            <span className="text-muted-foreground/70">{t('system.backups.col.size')}: </span>
            <span className="font-mono text-foreground">{formatBytes(backup.size)}</span>
          </span>
          <span>
            <span className="text-muted-foreground/70">{t('system.backups.col.generatedAt')}: </span>
            <span className="text-foreground">{generated}</span>
          </span>
          <span title={backup.sha256}>
            <span className="text-muted-foreground/70">{t('system.backups.col.sha256')}: </span>
            <span className="font-mono text-foreground">{sha256Short}…</span>
          </span>
        </div>
      </div>
      {canDownload ? (
        <a
          href={downloadUrl}
          download={backup.filename}
          aria-label={t('system.backups.download.aria', { filename: backup.filename })}
          className="inline-flex items-center justify-center rounded-md border border-input bg-background px-3 py-1 text-xs font-medium text-foreground shadow-sm transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring sm:self-start"
        >
          {t('system.backups.download')}
        </a>
      ) : (
        <span
          aria-disabled="true"
          title={t('common.role.required.adminOrAuditor')}
          className="inline-flex cursor-not-allowed items-center justify-center rounded-md border border-input bg-muted/50 px-3 py-1 text-xs font-medium text-muted-foreground shadow-sm sm:self-start"
        >
          {t('system.backups.download')}
        </span>
      )}
    </li>
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
  beforeLoad: () => requireRole('admin', 'auditor'),
  component: SystemPage,
})
