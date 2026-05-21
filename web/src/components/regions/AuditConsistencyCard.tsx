import { CheckCircle2, XCircle } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useT } from '@/i18n/t'
import { cn } from '@/lib/utils'

import { formatRelativeTime } from './RegionHealthCard'

import type { AuditChainHeadSHA } from '@/api/hooks'

// Phase 10.A-3 — AuditConsistencyCard.
//
// /regions 페이지에서 audit chain head sha 정합성을 표시:
//   - 현 region(self)의 head: seq + sha prefix(mono) + updatedAt.
//   - consistency 판정 (consistencyStatus 헬퍼): 모든 region head sha 동일 → consistent
//     (✅ green), 1개라도 다르면 → mismatch (❌ red) + 운영 runbook 안내.
//   - 본 round는 backend가 self region head만 노출하므로 카드 자체는 단일 head + 동일
//     판정을 표시. multi-PG 분기 시점에 호출자가 region 별 head 배열을 합성해 helper로
//     비교 가능 (helper는 단위 test에서 분기 검증).
//   - tooltip: 전체 sha hex(64자 mono).
//
// helpers (consistencyStatus / shortSha)는 export — useT 의존 없이 단위 test 분기 검증.

export type ConsistencyStatus = 'consistent' | 'mismatch' | 'empty'

// consistencyStatus — N개 head sha의 일관성 판정.
//   빈 배열 또는 모두 빈 문자열 → 'empty' (아직 audit entry 없음).
//   모두 동일 (비어있지 않은 sha 한 종류) → 'consistent'.
//   2종 이상 → 'mismatch'.
export function consistencyStatus(shas: ReadonlyArray<string>): ConsistencyStatus {
  const nonEmpty = shas.filter((s) => s && s.length > 0)
  if (nonEmpty.length === 0) return 'empty'
  const first = nonEmpty[0]
  for (const s of nonEmpty) {
    if (s !== first) return 'mismatch'
  }
  return 'consistent'
}

// shortSha — 64자 hex sha를 N자 prefix로 잘라 표기. 빈 입력은 빈 문자열.
export function shortSha(hex: string, prefix = 12): string {
  if (!hex) return ''
  return hex.slice(0, prefix)
}

interface AuditConsistencyCardProps {
  head: AuditChainHeadSHA | undefined
  selfRegion: string
  // 향후 multi-region 확장 시 호출자가 region 별 head sha를 합성해 전달.
  // 현 stage는 self head 1개만 — 자동으로 self head sha를 단일 원소 배열로 비교.
  peerShas?: ReadonlyArray<string>
}

export function AuditConsistencyCard({
  head,
  selfRegion,
  peerShas,
}: AuditConsistencyCardProps): React.ReactElement {
  const t = useT()
  const selfSha = head?.hashHex ?? ''
  const shas = peerShas ?? [selfSha]
  const status = consistencyStatus(shas)
  const isEmpty = status === 'empty'

  return (
    <Card data-card="audit-consistency">
      <CardHeader>
        <div className="flex items-center justify-between gap-2">
          <CardTitle className="text-base font-semibold">
            {t('regions.audit.title')}
          </CardTitle>
          <StatusBadge status={status} />
        </div>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        {isEmpty ? (
          <div className="text-xs text-muted-foreground">
            {t('regions.audit.empty')}
          </div>
        ) : (
          <>
            <div className="grid grid-cols-1 gap-1 sm:grid-cols-[12rem_1fr]">
              <span className="text-xs text-muted-foreground">
                {t('regions.self.label')}
              </span>
              <span className="text-xs font-medium text-foreground">
                {selfRegion || '—'}
              </span>
            </div>
            <div className="grid grid-cols-1 gap-1 sm:grid-cols-[12rem_1fr]">
              <span className="text-xs text-muted-foreground">
                {t('regions.audit.seq')}
              </span>
              <span className="text-xs font-medium tabular-nums">
                {head?.seq ?? 0}
              </span>
            </div>
            <div className="grid grid-cols-1 gap-1 sm:grid-cols-[12rem_1fr]">
              <span className="text-xs text-muted-foreground">
                {t('regions.audit.sha')}
              </span>
              <ShaCell hex={selfSha} />
            </div>
            {head?.updatedAt && (
              <div className="grid grid-cols-1 gap-1 sm:grid-cols-[12rem_1fr]">
                <span className="text-xs text-muted-foreground">
                  {t('regions.audit.updatedAt')}
                </span>
                <span className="text-xs text-foreground">
                  {formatRelativeTime(head.updatedAt)}
                </span>
              </div>
            )}
          </>
        )}
        {status === 'mismatch' && (
          <div
            className="mt-2 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-xs text-destructive"
            data-runbook-hint="audit-mismatch"
          >
            <div className="font-medium">{t('regions.audit.runbook')}</div>
            <div className="mt-0.5 font-mono">
              {t('regions.audit.runbook.path')}
            </div>
          </div>
        )}
        {status !== 'mismatch' && !isEmpty && (
          <div className="mt-2 text-[11px] text-muted-foreground">
            {t('regions.audit.selfNote')}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function StatusBadge({
  status,
}: {
  status: ConsistencyStatus
}): React.ReactElement {
  const t = useT()
  if (status === 'consistent') {
    return (
      <Badge
        variant="default"
        className={cn(
          'gap-1 bg-emerald-600 text-white hover:bg-emerald-600/90 dark:bg-emerald-700',
          'text-[10px] uppercase tracking-wide',
        )}
        data-consistency-status="consistent"
      >
        <CheckCircle2 className="h-3 w-3" aria-hidden />
        {t('regions.audit.consistent')}
      </Badge>
    )
  }
  if (status === 'mismatch') {
    return (
      <Badge
        variant="destructive"
        className="gap-1 text-[10px] uppercase tracking-wide"
        data-consistency-status="mismatch"
      >
        <XCircle className="h-3 w-3" aria-hidden />
        {t('regions.audit.mismatch')}
      </Badge>
    )
  }
  return (
    <Badge
      variant="outline"
      className="text-[10px] uppercase tracking-wide"
      data-consistency-status="empty"
    >
      —
    </Badge>
  )
}

function ShaCell({ hex }: { hex: string }): React.ReactElement {
  if (!hex) {
    return <span className="text-xs text-muted-foreground">—</span>
  }
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <span
            className="cursor-help break-all font-mono text-xs text-foreground"
            data-sha-prefix={shortSha(hex)}
          >
            {shortSha(hex)}…
          </span>
        </TooltipTrigger>
        <TooltipContent>
          <span className="break-all font-mono text-xs">{hex}</span>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
