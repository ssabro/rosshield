import { createFileRoute, Link } from '@tanstack/react-router'

import {
  Activity,
  AlertTriangle,
  ClipboardCheck,
  Gauge,
  ShieldCheck,
  Server,
} from 'lucide-react'

import {
  useAuditHead,
  useComplianceProfiles,
  useComplianceSnapshots,
  useInsights,
  useRobots,
  useScans,
} from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

import type { Insight, ScanSession } from '@/api/hooks'
import type { LucideIcon } from 'lucide-react'

// `/overview` — Lodestar 홈 대시보드.
//
// D-UI-1 Stage 4 — UX review P0 (actionable 정보 보강).
//   카드 6장으로 운영 핵심 지표를 한 화면에 압축한다 (1열 mobile / 2열 md / 3열 xl).
//   각 카드는 drill-down link 또는 CTA를 노출하여 다음 작업 후보를 명확히 한다.
//
//   1. 등록 로봇 수            → /robots
//   2. critical+high Insight   → /findings?severity=critical (강조 색)
//   3. 활성 Insight 전체       → /findings
//   4. 최근 24h 스캔 수        → /scans
//   5. 컴플라이언스 점수       → /compliance
//   6. 감사 체인 head          → /audit
//
//   로딩은 Skeleton, robot 0건 시 EmptyState로 "fleet에 로봇이 없습니다 → 등록 CTA" 안내.
function OverviewPage(): React.ReactElement {
  const t = useT()
  const robots = useRobots()
  const insights = useInsights({})
  const profiles = useComplianceProfiles()
  // 점수 카드는 첫 프로필의 최신 snapshot만 표시 — 여러 프로필이 있으면 사용자가 Compliance에서 확인.
  const firstProfileId = profiles.data?.[0]?.id
  const snapshots = useComplianceSnapshots(firstProfileId)
  const latestScore = snapshots.data?.[0]?.overallScore
  // 최근 세션 — 24h 윈도 계산은 카드 안에서. polling 미적용 (홈 부담 회피).
  const scans = useScans({ limit: 50 })
  const auditHead = useAuditHead()

  const robotsReady = !robots.isPending && !robots.isError
  const hasRobots = robotsReady && (robots.data?.length ?? 0) > 0
  const showEmpty = robotsReady && !hasRobots

  // ── 상세 메트릭 ────────────────────────────────────────────────
  // Insight: critical + high 합산. unknown severity는 무시 (방어).
  const criticalHighCount = insights.data
    ? countByCriticalOrHigh(insights.data)
    : null

  // 24h 윈도: createdAt 또는 startedAt 기준 24시간 이내. terminal 외(=running/pending) 카운트도 별도.
  const scans24h = scans.data ? countRecentScans(scans.data, 24) : null
  const activeScans = scans.data ? countActiveScans(scans.data) : null

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.overview.title')}
        description={t('pages.overview.description')}
      />

      {showEmpty ? (
        // D-UI-1 Stage 4 — 빈 fleet 시 첫 사용자 가이드 (robot 등록 → fleet → scan).
        <EmptyState
          variant="no-data"
          size="lg"
          icon={Server}
          title={t('overview.empty.title')}
          description={t('overview.empty.description')}
          action={
            <div className="flex flex-wrap items-center gap-2">
              <Button asChild size="sm">
                <Link to="/robots">{t('overview.empty.cta.registerRobot')}</Link>
              </Button>
              <Button asChild size="sm" variant="outline">
                <Link to="/fleets">{t('overview.empty.cta.viewFleets')}</Link>
              </Button>
            </div>
          }
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          <SummaryCard
            icon={Server}
            title={t('overview.card.robots')}
            value={robots.isPending ? null : (robots.data?.length ?? 0).toString()}
            to="/robots"
            ctaKey="overview.card.robots.cta"
          />
          <CriticalHighCard
            count={insights.isPending ? null : criticalHighCount}
          />
          <SummaryCard
            icon={AlertTriangle}
            title={t('overview.card.findings')}
            value={
              insights.isPending ? null : (insights.data?.length ?? 0).toString()
            }
            to="/findings"
            ctaKey="overview.card.findings.cta"
          />
          <Scans24hCard
            count={scans.isPending ? null : scans24h}
            active={scans.isPending ? null : activeScans}
          />
          <SummaryCard
            icon={ClipboardCheck}
            title={t('overview.card.compliance')}
            value={
              profiles.isPending ? null : (profiles.data?.length ?? 0).toString()
            }
            to="/compliance"
            ctaKey="overview.card.compliance.cta"
          />
          <ScoreSummaryCard
            latestScore={latestScore}
            hasProfile={!!firstProfileId}
            loading={profiles.isPending || (!!firstProfileId && snapshots.isPending)}
          />
          <AuditHeadCard
            seq={auditHead.data?.seq}
            hashHex={auditHead.data?.hashHex}
            loading={auditHead.isPending}
            error={auditHead.isError}
          />
        </div>
      )}
    </div>
  )
}

// SummaryCard — value가 null이면 Skeleton placeholder. 모든 카드의 base layout.
function SummaryCard({
  icon: Icon,
  title,
  value,
  to,
  ctaKey,
}: {
  icon: LucideIcon
  title: string
  value: string | null
  to: '/robots' | '/findings' | '/compliance' | '/scans' | '/audit'
  ctaKey:
    | 'overview.card.robots.cta'
    | 'overview.card.findings.cta'
    | 'overview.card.compliance.cta'
    | 'overview.card.scans24h.cta'
    | 'overview.card.auditHead.cta'
    | 'overview.card.criticalHigh.cta'
}): React.ReactElement {
  const t = useT()
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {title}
        </CardTitle>
        <Icon className="h-4 w-4 text-muted-foreground" aria-hidden />
      </CardHeader>
      <CardContent className="space-y-2">
        {value === null ? (
          <Skeleton className="h-9 w-16" />
        ) : (
          <p className="text-3xl font-semibold tracking-tight">{value}</p>
        )}
        <Button asChild variant="outline" size="sm">
          <Link to={to}>{t(ctaKey)}</Link>
        </Button>
      </CardContent>
    </Card>
  )
}

// CriticalHighCard — critical + high 합. >0이면 destructive 강조, 0이면 정상 표시.
function CriticalHighCard({
  count,
}: {
  count: number | null
}): React.ReactElement {
  const t = useT()
  const isCritical = count !== null && count > 0
  return (
    <Card className={isCritical ? 'border-destructive/50' : undefined}>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('overview.card.criticalHigh')}
        </CardTitle>
        <ShieldCheck
          className={`h-4 w-4 ${isCritical ? 'text-destructive' : 'text-muted-foreground'}`}
          aria-hidden
        />
      </CardHeader>
      <CardContent className="space-y-2">
        {count === null ? (
          <Skeleton className="h-9 w-16" />
        ) : count === 0 ? (
          <p className="text-3xl font-semibold tracking-tight text-muted-foreground">
            {t('overview.card.criticalHigh.none')}
          </p>
        ) : (
          <p className="text-3xl font-semibold tracking-tight text-destructive">
            {count}
          </p>
        )}
        <Button
          asChild
          variant={isCritical ? 'destructive' : 'outline'}
          size="sm"
        >
          {/* findings 페이지에 URL 기반 severity 필터 동기화는 미지원(현 hook 구조 그대로). */}
          {/* drill-down 우선 — 사용자는 진입 후 severity dropdown으로 좁힌다. */}
          <Link to="/findings">{t('overview.card.criticalHigh.cta')}</Link>
        </Button>
      </CardContent>
    </Card>
  )
}

// Scans24hCard — 최근 24시간 시작된 세션 수 + 진행 중(active) 수 표시.
function Scans24hCard({
  count,
  active,
}: {
  count: number | null
  active: number | null
}): React.ReactElement {
  const t = useT()
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('overview.card.scans24h')}
        </CardTitle>
        <Activity className="h-4 w-4 text-muted-foreground" aria-hidden />
      </CardHeader>
      <CardContent className="space-y-2">
        {count === null ? (
          <Skeleton className="h-9 w-24" />
        ) : (
          <p className="text-3xl font-semibold tracking-tight">
            {count}
            {active !== null && active > 0 && (
              <span className="ml-1 text-sm font-normal text-muted-foreground">
                {t('overview.card.scans24h.activeSuffix', {
                  active: active.toString(),
                })}
              </span>
            )}
          </p>
        )}
        <Button asChild variant="outline" size="sm">
          <Link to="/scans">{t('overview.card.scans24h.cta')}</Link>
        </Button>
      </CardContent>
    </Card>
  )
}

// AuditHeadCard — 감사 체인 현재 head seq + hash prefix. 비어있으면 genesis 표시.
function AuditHeadCard({
  seq,
  hashHex,
  loading,
  error,
}: {
  seq: number | undefined
  hashHex: string | undefined
  loading: boolean
  error: boolean
}): React.ReactElement {
  const t = useT()
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('overview.card.auditHead')}
        </CardTitle>
        <Gauge className="h-4 w-4 text-muted-foreground" aria-hidden />
      </CardHeader>
      <CardContent className="space-y-2">
        {loading ? (
          <Skeleton className="h-9 w-32" />
        ) : error || seq === undefined ? (
          <CardDescription>{t('overview.card.auditHead.empty')}</CardDescription>
        ) : (
          <div className="space-y-0.5">
            <p className="text-3xl font-semibold tracking-tight tabular-nums">
              #{seq}
            </p>
            {hashHex && (
              <p
                className="truncate font-mono text-xs text-muted-foreground"
                title={hashHex}
              >
                {hashHex.slice(0, 16)}…
              </p>
            )}
          </div>
        )}
        <Button asChild variant="outline" size="sm">
          <Link to="/audit">{t('overview.card.auditHead.cta')}</Link>
        </Button>
      </CardContent>
    </Card>
  )
}

function ScoreSummaryCard({
  latestScore,
  hasProfile,
  loading,
}: {
  latestScore: number | undefined
  hasProfile: boolean
  loading: boolean
}): React.ReactElement {
  const t = useT()
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('overview.card.score')}
        </CardTitle>
        <Gauge className="h-4 w-4 text-muted-foreground" aria-hidden />
      </CardHeader>
      <CardContent className="space-y-2">
        {loading ? (
          <Skeleton className="h-9 w-24" />
        ) : !hasProfile ? (
          <>
            <CardDescription>{t('overview.card.score.empty')}</CardDescription>
            <Button asChild variant="outline" size="sm">
              <Link to="/compliance">{t('overview.card.score.cta')}</Link>
            </Button>
          </>
        ) : latestScore == null ? (
          <p className="text-3xl font-semibold tracking-tight">—</p>
        ) : (
          <div className="flex items-baseline gap-2">
            <p className="text-3xl font-semibold tracking-tight">
              {(latestScore * 100).toFixed(1)}%
            </p>
            <Badge variant={latestScore >= 0.9 ? 'default' : latestScore >= 0.7 ? 'secondary' : 'destructive'}>
              {latestScore >= 0.9 ? '우수' : latestScore >= 0.7 ? '양호' : '미흡'}
            </Badge>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

// ── pure helpers (단위 테스트 가능) ─────────────────────────────

// countByCriticalOrHigh — insight 목록에서 severity가 critical 또는 high인 항목 수.
export function countByCriticalOrHigh(insights: Insight[]): number {
  let n = 0
  for (const ins of insights) {
    if (ins.severity === 'critical' || ins.severity === 'high') n++
  }
  return n
}

// countRecentScans — 지난 hours 시간 내 createdAt(또는 startedAt) 있는 세션 수.
//   서버는 보통 createdAt을 채우지만, 일부 케이스에 startedAt만 있을 수 있어 fallback 처리.
//   invalid date는 무시.
export function countRecentScans(scans: ScanSession[], hours: number): number {
  const cutoff = Date.now() - hours * 60 * 60 * 1000
  let n = 0
  for (const s of scans) {
    const iso = s.createdAt ?? s.startedAt ?? undefined
    if (!iso) continue
    const t = Date.parse(iso)
    if (Number.isNaN(t)) continue
    if (t >= cutoff) n++
  }
  return n
}

// countActiveScans — pending/running 상태 세션 수.
export function countActiveScans(scans: ScanSession[]): number {
  let n = 0
  for (const s of scans) {
    if (s.status === 'pending' || s.status === 'running') n++
  }
  return n
}

export const Route = createFileRoute('/_authenticated/overview')({
  component: OverviewPage,
})
