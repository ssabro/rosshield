import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { useT } from '@/i18n/t'
import { cn } from '@/lib/utils'

import type { RegionReplica } from '@/api/hooks'
import type { DictKey } from '@/i18n/dict'

// Phase 10.A-2 — RegionHealthCard.
//
// /regions 페이지에서 region 1개를 카드 형태로 표시:
//   - region 이름 + role badge (primary=초록 / standby=파랑 / failed=빨강)
//   - endpoint (mono font)
//   - replication lag (lagSeconds) + 색상 코드:
//       lag ≤ 5s   → green   "정상"
//       5 < lag ≤ 30s → yellow "주의"
//       lag > 30s  → red     "지연"
//       lag == -1  → muted   "알 수 없음"
//   - last replay at + last heartbeat at (locale 표기)
//
// helpers (lagBucket / roleBadgeVariant / roleLabelKey 등)는 export — 단위 test에서
// useT 의존 없이 분기 검증.

export type RoleBadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline'

// roleBadgeVariant — role string → shadcn Badge variant.
//   primary → default(초록 톤은 별 className 부착) · standby → secondary(파랑)
//   failed → destructive(빨강) · 그 외 → outline.
export function roleBadgeVariant(role: string): RoleBadgeVariant {
  switch (role.toLowerCase()) {
    case 'primary':
      return 'default'
    case 'standby':
      return 'secondary'
    case 'failed':
      return 'destructive'
    default:
      return 'outline'
  }
}

// roleLabelKey — role string → dict 키 (regions.role.*). 미지정/처음 보는 값은 unknown.
export function roleLabelKey(role: string): DictKey {
  switch (role.toLowerCase()) {
    case 'primary':
      return 'regions.role.primary'
    case 'standby':
      return 'regions.role.standby'
    case 'failed':
      return 'regions.role.failed'
    default:
      return 'regions.role.unknown'
  }
}

// roleBadgeClassName — role별 시각 강조(badge 자체 색은 variant 기본을 따라가지만
// primary는 emerald, standby는 sky로 명확 구분).
export function roleBadgeClassName(role: string): string {
  switch (role.toLowerCase()) {
    case 'primary':
      return 'bg-emerald-600 text-white hover:bg-emerald-600/90 dark:bg-emerald-700'
    case 'standby':
      return 'bg-sky-600 text-white hover:bg-sky-600/90 dark:bg-sky-700'
    case 'failed':
      return ''
    default:
      return ''
  }
}

export type LagBucket = 'healthy' | 'warning' | 'delayed' | 'unknown'

// lagBucket — lagSeconds → 4 분기.
//   -1 (last_replay_at zero) → unknown
//   0 ≤ lag ≤ 5    → healthy (green)
//   5 < lag ≤ 30   → warning (yellow)
//   lag > 30       → delayed (red)
export function lagBucket(lagSeconds: number): LagBucket {
  if (lagSeconds < 0) return 'unknown'
  if (lagSeconds <= 5) return 'healthy'
  if (lagSeconds <= 30) return 'warning'
  return 'delayed'
}

// lagLabelKey — bucket → dict 키.
export function lagLabelKey(bucket: LagBucket): DictKey {
  switch (bucket) {
    case 'healthy':
      return 'regions.lag.healthy'
    case 'warning':
      return 'regions.lag.warning'
    case 'delayed':
      return 'regions.lag.delayed'
    case 'unknown':
      return 'regions.lag.unknown'
  }
}

// lagTextClassName — bucket → Tailwind text 색.
export function lagTextClassName(bucket: LagBucket): string {
  switch (bucket) {
    case 'healthy':
      return 'text-emerald-600 dark:text-emerald-400'
    case 'warning':
      return 'text-amber-600 dark:text-amber-400'
    case 'delayed':
      return 'text-red-600 dark:text-red-400'
    case 'unknown':
      return 'text-muted-foreground'
  }
}

// formatRelativeTime — ISO 시각 → "N초 전" / "N분 전" / "N시간 전" 형식.
//   nowMs 인자는 test 결정성 — undefined면 Date.now() 사용. invalid input은 빈 문자열.
export function formatRelativeTime(
  iso: string | undefined,
  nowMs?: number,
): string {
  if (!iso) return ''
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  const now = nowMs ?? Date.now()
  const diff = Math.max(0, Math.floor((now - t) / 1000))
  if (diff < 60) return `${diff}초 전`
  const m = Math.floor(diff / 60)
  if (m < 60) return `${m}분 전`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}시간 전`
  const d = Math.floor(h / 24)
  return `${d}일 전`
}

interface RegionHealthCardProps {
  replica: RegionReplica
}

export function RegionHealthCard({
  replica,
}: RegionHealthCardProps): React.ReactElement {
  const t = useT()
  const bucket = lagBucket(replica.lagSeconds)
  const lagText = lagTextClassName(bucket)
  const roleClass = roleBadgeClassName(replica.role)
  const lagDisplay =
    bucket === 'unknown' ? '—' : `${replica.lagSeconds}s`

  return (
    <Card data-region={replica.region}>
      <CardHeader>
        <div className="flex items-center justify-between gap-2">
          <CardTitle className="text-base font-semibold">
            {replica.region}
          </CardTitle>
          <Badge
            variant={roleBadgeVariant(replica.role)}
            className={cn('text-[10px] uppercase tracking-wide', roleClass)}
            data-role={replica.role.toLowerCase()}
          >
            {t(roleLabelKey(replica.role))}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        <Row label={t('regions.endpoint')} value={replica.endpoint} mono />
        <div className="grid grid-cols-1 gap-1 sm:grid-cols-[12rem_1fr]">
          <span className="text-xs text-muted-foreground">
            {t('regions.lag.label')}
          </span>
          <span
            className={cn('text-xs font-medium tabular-nums', lagText)}
            data-lag-bucket={bucket}
          >
            {lagDisplay} · {t(lagLabelKey(bucket))}
          </span>
        </div>
        {replica.lastReplayAt && (
          <Row
            label={t('regions.lastReplay')}
            value={formatRelativeTime(replica.lastReplayAt)}
          />
        )}
        {replica.lastHeartbeatAt && (
          <Row
            label={t('regions.lastHeartbeat')}
            value={formatRelativeTime(replica.lastHeartbeatAt)}
          />
        )}
      </CardContent>
    </Card>
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
