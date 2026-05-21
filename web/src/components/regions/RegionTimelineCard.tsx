import { ArrowRight, Clock } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { useT } from '@/i18n/t'
import { cn } from '@/lib/utils'

import { formatRelativeTime } from './RegionHealthCard'

import type { FailoverEvent } from '@/api/hooks'
import type { DictKey } from '@/i18n/dict'

// Phase 10.A-4 — RegionTimelineCard.
//
// /regions 페이지에서 최근 region cutover 이력을 timeline 시각화:
//   - 위에서 아래로 시간순 (initiated DESC) — backend가 이미 정렬해서 반환.
//   - 각 entry: dot + line + relative time + from → to + status badge + actor + reason.
//   - status 3종: in-progress(amber) / completed(emerald) / failed(red — 향후 별 path).
//   - empty state: "cutover 이력 없음".
//   - read-only (failover trigger UI는 별 epic, design doc §6.4 명시).
//
// helpers (statusBadgeVariant / statusLabelKey / statusBadgeClassName)는 export —
// useT 의존 없이 단위 test 분기 검증.

export type FailoverStatus = FailoverEvent['status']

export type StatusBadgeVariant =
  | 'default'
  | 'secondary'
  | 'destructive'
  | 'outline'

// statusBadgeVariant — failover status → shadcn Badge variant.
export function statusBadgeVariant(status: FailoverStatus): StatusBadgeVariant {
  switch (status) {
    case 'completed':
      return 'default'
    case 'in-progress':
      return 'secondary'
    case 'failed':
      return 'destructive'
    default:
      return 'outline'
  }
}

// statusLabelKey — failover status → dict 키.
export function statusLabelKey(status: FailoverStatus): DictKey {
  switch (status) {
    case 'completed':
      return 'regions.timeline.status.completed'
    case 'in-progress':
      return 'regions.timeline.status.inProgress'
    case 'failed':
      return 'regions.timeline.status.failed'
  }
}

// statusBadgeClassName — status별 색상 강조.
export function statusBadgeClassName(status: FailoverStatus): string {
  switch (status) {
    case 'completed':
      return 'bg-emerald-600 text-white hover:bg-emerald-600/90 dark:bg-emerald-700'
    case 'in-progress':
      return 'bg-amber-500 text-white hover:bg-amber-500/90 dark:bg-amber-600'
    case 'failed':
      return ''
  }
}

// statusDotClassName — timeline dot 색.
export function statusDotClassName(status: FailoverStatus): string {
  switch (status) {
    case 'completed':
      return 'bg-emerald-500'
    case 'in-progress':
      return 'bg-amber-500'
    case 'failed':
      return 'bg-red-500'
  }
}

interface RegionTimelineCardProps {
  events: ReadonlyArray<FailoverEvent>
  updatedAt?: string
}

export function RegionTimelineCard({
  events,
  updatedAt,
}: RegionTimelineCardProps): React.ReactElement {
  const t = useT()
  return (
    <Card data-card="region-timeline">
      <CardHeader>
        <div className="flex items-center justify-between gap-2">
          <CardTitle className="text-base font-semibold">
            {t('regions.timeline.title')}
          </CardTitle>
          {updatedAt && (
            <span className="text-[10px] text-muted-foreground">
              {t('regions.timeline.updatedAt')}: {formatRelativeTime(updatedAt)}
            </span>
          )}
        </div>
      </CardHeader>
      <CardContent>
        {events.length === 0 ? (
          <EmptyTimeline />
        ) : (
          <ol
            className="relative ml-2 space-y-4 border-l border-border pl-4"
            data-timeline-list
          >
            {events.map((event) => (
              <TimelineEntry key={event.id} event={event} />
            ))}
          </ol>
        )}
      </CardContent>
    </Card>
  )
}

function EmptyTimeline(): React.ReactElement {
  const t = useT()
  return (
    <div className="flex items-center gap-2 text-xs text-muted-foreground">
      <Clock className="h-3.5 w-3.5" aria-hidden />
      {t('regions.timeline.empty')}
    </div>
  )
}

function TimelineEntry({
  event,
}: {
  event: FailoverEvent
}): React.ReactElement {
  const t = useT()
  const dotClass = statusDotClassName(event.status)
  const badgeClass = statusBadgeClassName(event.status)
  const when = event.completedAt ?? event.initiatedAt

  return (
    <li className="relative" data-failover-id={event.id}>
      <span
        className={cn(
          'absolute -left-[1.4rem] top-1 h-2.5 w-2.5 rounded-full ring-2 ring-background',
          dotClass,
        )}
        data-status-dot={event.status}
        aria-hidden
      />
      <div className="flex flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted-foreground tabular-nums">
            {formatRelativeTime(when)}
          </span>
          <span className="inline-flex items-center gap-1 text-xs font-medium">
            <span className="font-mono">{event.fromRegion}</span>
            <ArrowRight className="h-3 w-3 text-muted-foreground" aria-hidden />
            <span className="font-mono">{event.toRegion}</span>
          </span>
          <Badge
            variant={statusBadgeVariant(event.status)}
            className={cn('text-[10px] uppercase tracking-wide', badgeClass)}
            data-status={event.status}
          >
            {t(statusLabelKey(event.status))}
          </Badge>
        </div>
        {event.initiatedByUser && (
          <div className="text-[11px] text-muted-foreground">
            <span className="mr-1">{t('regions.timeline.actor')}:</span>
            <span className="font-mono text-foreground">
              {event.initiatedByUser}
            </span>
          </div>
        )}
        {event.reason && (
          <div className="text-[11px] text-muted-foreground">
            <span className="mr-1">{t('regions.timeline.reason')}:</span>
            <span className="text-foreground">{event.reason}</span>
          </div>
        )}
      </div>
    </li>
  )
}
