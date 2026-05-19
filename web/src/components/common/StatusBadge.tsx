import {
  CheckCircle2,
  Circle,
  CircleDashed,
  Clock,
  Pause,
  XCircle,
  type LucideIcon,
} from 'lucide-react'

import { cn } from '@/lib/utils'

// D-UI-1 Stage 2 — StatusBadge.
//
// 사용처: scan/job/health/connection status column 등. Severity와 달리 다양한
// 도메인 status를 한 컴포넌트가 처리할 수 있도록 status string + variant 매핑.
//
// running 상태는 animated dot로 진행 중임을 즉시 인지 가능 (a11y: aria-live는
// 호출지에서 row 단위로 부여).
//
// status 매핑 (확장 가능):
//   running     → blue · animated dot
//   pending     → gray · pulse
//   queued      → gray · dashed
//   success/ok  → green · check
//   failed/error → red · x
//   paused      → amber · pause
//   unknown     → gray · circle

export type StatusKind =
  | 'running'
  | 'pending'
  | 'queued'
  | 'success'
  | 'failed'
  | 'paused'
  | 'unknown'

interface StatusConfig {
  icon: LucideIcon
  className: string
  defaultLabel: string
  animated?: boolean
}

const STATUS_MAP: Record<StatusKind, StatusConfig> = {
  running: {
    icon: Circle,
    className:
      'bg-sky-100 text-sky-900 dark:bg-sky-950 dark:text-sky-200 border-sky-300/40',
    defaultLabel: '진행 중',
    animated: true,
  },
  pending: {
    icon: Clock,
    className:
      'bg-slate-100 text-slate-700 dark:bg-slate-900 dark:text-slate-300 border-slate-300/40',
    defaultLabel: '대기',
  },
  queued: {
    icon: CircleDashed,
    className:
      'bg-slate-100 text-slate-700 dark:bg-slate-900 dark:text-slate-300 border-slate-300/40',
    defaultLabel: '큐 대기',
  },
  success: {
    icon: CheckCircle2,
    className:
      'bg-emerald-100 text-emerald-900 dark:bg-emerald-950 dark:text-emerald-200 border-emerald-300/40',
    defaultLabel: '성공',
  },
  failed: {
    icon: XCircle,
    className:
      'bg-red-100 text-red-900 dark:bg-red-950 dark:text-red-200 border-red-300/40',
    defaultLabel: '실패',
  },
  paused: {
    icon: Pause,
    className:
      'bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200 border-amber-300/40',
    defaultLabel: '일시 정지',
  },
  unknown: {
    icon: Circle,
    className:
      'bg-muted text-muted-foreground border-border',
    defaultLabel: '알 수 없음',
  },
}

interface StatusBadgeProps {
  status: StatusKind
  // 라벨 override (i18n key를 호출지에서 t()로 풀어서 전달).
  label?: string
  showIcon?: boolean
  className?: string
}

export function StatusBadge({
  status,
  label,
  showIcon = true,
  className,
}: StatusBadgeProps): React.ReactElement {
  const cfg = STATUS_MAP[status]
  const Icon = cfg.icon
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded border px-2 py-0.5 text-xs font-medium',
        cfg.className,
        className,
      )}
      data-status={status}
    >
      {showIcon &&
        (cfg.animated ? (
          <span className="relative inline-flex h-2 w-2" aria-hidden>
            <span className="absolute inset-0 rounded-full bg-current opacity-75 motion-safe:animate-ping" />
            <span className="relative inline-flex h-2 w-2 rounded-full bg-current" />
          </span>
        ) : (
          <Icon className="size-3" aria-hidden />
        ))}
      {label ?? cfg.defaultLabel}
    </span>
  )
}
