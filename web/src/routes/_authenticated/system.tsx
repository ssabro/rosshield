import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'

import { CalendarClock, ServerOff } from 'lucide-react'

import { ApiError } from '@/api/errors'
import { API_BASE_PATH } from '@/api/client'
import { useBackups, useHasPermission, useLicenseInfo, usePacks, useScans, useUsageStats } from '@/api/hooks'
import { StatusBadge, type StatusKind } from '@/components/common/StatusBadge'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { requirePermission } from '@/lib/route-guards'
import { formatBytes } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

import type { DictKey } from '@/i18n/dict'

// `/system` — B6+B7 운영 정보 dashboard.
//
// D-UI-1 Stage 4 — PageHeader 표준화 + StatusBadge / EmptyState / Skeleton 일관
// 패턴 적용 + 4 핵심 카드 (Health · HA · License · Backups) grid polish. 부가 카드
// (Usage / Severity / Packs)는 full-width로 두어 정보 밀도 유지. hook · API ·
// business logic은 무변경.
//
//   - Service health (healthz fetch, public)
//   - HA status (healthz.ha 필드 — E25)
//   - License usage (useLicenseInfo, E24)
//   - Backups CLI 안내 (E28; 자동 백업·web 다운로드는 B7 후속)

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

      {/* 핵심 4 카드 — 헬스 · HA · 라이선스 · 백업. 모바일 1열, lg 이상 2열. */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <HealthCard />
        <HACard />
        <LicenseUsageCard />
        <BackupsCard />
      </div>

      {/* 부가 카드 — 운영 메트릭. full-width로 정보 밀도 유지. */}
      <UsageStatsCard />
      <ScansSeverityCard />
      <PacksCard />
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
          <CardContentSkeleton rows={3} />
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
        {q.isPending && <CardContentSkeleton rows={4} />}
        {q.isError && (
          <p className="text-destructive" role="alert">
            {t('system.health.error')}
          </p>
        )}
        {q.isSuccess && (
          <>
            <div className="flex items-center gap-2">
              <StatusBadge
                status={healthStatusToBadgeKind(q.data.status)}
                label={t(healthStatusLabelKey(q.data.status))}
              />
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
        {q.isPending && <CardContentSkeleton rows={3} />}
        {q.isError && (
          <p className="text-destructive" role="alert">
            {t('system.health.error')}
          </p>
        )}
        {q.isSuccess && !ha && (
          <EmptyState
            icon={ServerOff}
            size="sm"
            title={t('system.ha.disabled.title')}
            description={t('system.ha.disabled.description')}
            className="border-dashed"
          />
        )}
        {ha && (
          <>
            <div className="flex items-center gap-2">
              <StatusBadge
                status={ha.role === 'leader' ? 'success' : 'pending'}
                label={t(haRoleLabelKey(ha.role))}
              />
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
        {q.isPending && <CardContentSkeleton rows={3} />}
        {q.isError && (
          <p className="text-destructive" role="alert">
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
        {q.isPending && <CardContentSkeleton rows={3} />}
        {q.isError && (
          <p className="text-destructive" role="alert">
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
        {q.isPending && <CardContentSkeleton rows={2} />}
        {q.isError && (
          <p className="text-destructive" role="alert">
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
  // RBAC Stage 5 — backup download은 system.read (§2.2 ID 19, admin/auditor 묶음).
  const canDownload = useHasPermission('system', 'read')
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

          {q.isPending && <CardContentSkeleton rows={3} />}
          {q.isError && (
            <p className="text-xs text-destructive" role="alert">
              {q.error instanceof ApiError
                ? q.error.message
                : t('system.backups.list.error')}
            </p>
          )}
          {q.isSuccess && recent.length === 0 && (
            <EmptyState
              icon={CalendarClock}
              size="sm"
              title={t('system.backups.empty.title')}
              description={t('system.backups.empty.description')}
              className="border-dashed"
            />
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

// ────────────────────────────────────────────────────────────────────────
// Helpers (export — 단위 테스트용)
// ────────────────────────────────────────────────────────────────────────

// healthStatusToBadgeKind — /healthz status 문자열 → StatusBadge `StatusKind`.
//   백엔드 enum: 'ok' | 'degraded' (internal/api/gen/openapi.gen.go §HealthStatusStatus).
//   'fail'은 일부 sub-component (signer 등) 에서만 — 카드 전체 status는 ok/degraded.
//   미지정 또는 처음 보는 값은 'unknown' 으로 안전 fallback.
export function healthStatusToBadgeKind(status: string): StatusKind {
  switch (status.toLowerCase()) {
    case 'ok':
      return 'success'
    case 'degraded':
      return 'paused'
    case 'fail':
    case 'down':
      return 'failed'
    default:
      return 'unknown'
  }
}

// healthStatusLabelKey — /healthz status 문자열 → 사용자 라벨 dict 키.
export function healthStatusLabelKey(status: string): DictKey {
  switch (status.toLowerCase()) {
    case 'ok':
      return 'system.health.status.ok'
    case 'degraded':
      return 'system.health.status.degraded'
    case 'fail':
    case 'down':
      return 'system.health.status.fail'
    default:
      return 'system.health.status.unknown'
  }
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

// CardContentSkeleton — Card body 내부에 들어가는 가벼운 skeleton (헤더는 이미 표시).
//   CardSkeleton 컴포넌트는 외곽 border까지 렌더하므로 Card 내부에서는 부적합.
function CardContentSkeleton({ rows = 3 }: { rows?: number }): React.ReactElement {
  return (
    <div className="space-y-2" role="status" aria-label="불러오는 중">
      <Skeleton className="h-5 w-1/4" />
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} className={i === rows - 1 ? 'h-3 w-2/3' : 'h-3 w-full'} />
      ))}
    </div>
  )
}

export const Route = createFileRoute('/_authenticated/system')({
  beforeLoad: () => requirePermission('system', 'read'),
  component: SystemPage,
})
