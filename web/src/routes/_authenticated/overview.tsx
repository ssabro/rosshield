import { createFileRoute, Link } from '@tanstack/react-router'

import {
  AlertTriangle,
  ClipboardCheck,
  Gauge,
  Server,
} from 'lucide-react'

import {
  useComplianceProfiles,
  useComplianceSnapshots,
  useInsights,
  useRobots,
} from '@/api/hooks'
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

import type { LucideIcon } from 'lucide-react'

// `/overview` — 헤드라인 카드 4개로 핵심 지표를 요약합니다 (B1).
//   - 등록 로봇 수 (useRobots)
//   - 활성 Insight 수 (useInsights)
//   - 컴플라이언스 프로필 수 (useComplianceProfiles)
//   - 최근 컴플라이언스 점수 (첫 프로필의 최신 snapshot)
function OverviewPage(): React.ReactElement {
  const t = useT()
  const robots = useRobots()
  const insights = useInsights({})
  const profiles = useComplianceProfiles()
  // 점수 카드는 첫 프로필의 최신 snapshot만 표시 — 여러 프로필이 있으면 사용자가 Compliance에서 확인.
  const firstProfileId = profiles.data?.[0]?.id
  const snapshots = useComplianceSnapshots(firstProfileId)
  const latestScore = snapshots.data?.[0]?.overallScore

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.overview.title')}
        description={t('pages.overview.description')}
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
        <SummaryCard
          icon={Server}
          title={t('overview.card.robots')}
          value={robots.isPending ? '…' : (robots.data?.length ?? 0).toString()}
          to="/robots"
          ctaKey="overview.card.robots.cta"
        />
        <SummaryCard
          icon={AlertTriangle}
          title={t('overview.card.findings')}
          value={
            insights.isPending ? '…' : (insights.data?.length ?? 0).toString()
          }
          to="/findings"
          ctaKey="overview.card.findings.cta"
        />
        <SummaryCard
          icon={ClipboardCheck}
          title={t('overview.card.compliance')}
          value={
            profiles.isPending ? '…' : (profiles.data?.length ?? 0).toString()
          }
          to="/compliance"
          ctaKey="overview.card.compliance.cta"
        />
        <ScoreSummaryCard latestScore={latestScore} hasProfile={!!firstProfileId} />
      </div>
    </div>
  )
}

function SummaryCard({
  icon: Icon,
  title,
  value,
  to,
  ctaKey,
}: {
  icon: LucideIcon
  title: string
  value: string
  to: '/robots' | '/findings' | '/compliance'
  ctaKey: 'overview.card.robots.cta' | 'overview.card.findings.cta' | 'overview.card.compliance.cta'
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
        <p className="text-3xl font-semibold tracking-tight">{value}</p>
        <Button asChild variant="outline" size="sm">
          <Link to={to}>{t(ctaKey)}</Link>
        </Button>
      </CardContent>
    </Card>
  )
}

function ScoreSummaryCard({
  latestScore,
  hasProfile,
}: {
  latestScore: number | undefined
  hasProfile: boolean
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
        {!hasProfile ? (
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

export const Route = createFileRoute('/_authenticated/overview')({
  component: OverviewPage,
})
