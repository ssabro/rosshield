// D-UI-1 Stage 1 — severity·status helper.
//
// 목적: UI 컴포넌트가 severity·status 색상·아이콘·label을 일관되게 끌어쓰도록
// 단일 source 제공. design token(`bg-severity-*`, `text-status-*`)은 globals.css의
// @theme 블록에 정의되며 Tailwind v4가 자동으로 utility로 노출한다.
//
// 사용 예 (Stage 2 SeverityBadge·StatusBadge에서):
//   import { severityClassName, severityIcon, severityLabel } from '@/lib/severity'
//   import { t } from '@/i18n/t'
//   const Icon = severityIcon[level]
//   return <span className={severityClassName[level]}>
//     <Icon className="h-3 w-3" /> {t(severityLabel[level])}
//   </span>

import {
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  Clock,
  Info,
  Loader2,
  ShieldAlert,
  ShieldOff,
  TrendingDown,
  XCircle,
  type LucideIcon,
} from 'lucide-react'

import type { DictKey } from '@/i18n/dict'

export type Severity = 'critical' | 'high' | 'medium' | 'low' | 'info'

export type Status =
  | 'running'
  | 'pending'
  | 'completed'
  | 'failed'
  | 'cancelled'

export const SEVERITY_LEVELS: readonly Severity[] = [
  'critical',
  'high',
  'medium',
  'low',
  'info',
] as const

export const STATUS_LEVELS: readonly Status[] = [
  'running',
  'pending',
  'completed',
  'failed',
  'cancelled',
] as const

// Badge 배경 + foreground(자동 흰색) — solid variant. WCAG AA 4.5:1 가정 (light 색상은
// L 40~50 으로 흰색 글자, dark variant는 L 65 로 짙은 글자가 contrast 확보).
// solid variant는 강조용. subtle variant는 별 helper에서 제공할 수 있다 (Stage 2 이후).
export const severityClassName: Record<Severity, string> = {
  critical: 'bg-severity-critical text-white dark:text-slate-950',
  high: 'bg-severity-high text-white dark:text-slate-950',
  medium: 'bg-severity-medium text-white dark:text-slate-950',
  low: 'bg-severity-low text-white dark:text-slate-950',
  info: 'bg-severity-info text-white dark:text-slate-950',
}

// 텍스트 전용 (배경 없음) — table cell, inline label 등.
export const severityTextClassName: Record<Severity, string> = {
  critical: 'text-severity-critical',
  high: 'text-severity-high',
  medium: 'text-severity-medium',
  low: 'text-severity-low',
  info: 'text-severity-info',
}

export const statusClassName: Record<Status, string> = {
  running: 'bg-status-running text-white dark:text-slate-950',
  pending: 'bg-status-pending text-white dark:text-slate-950',
  completed: 'bg-status-completed text-white dark:text-slate-950',
  failed: 'bg-status-failed text-white dark:text-slate-950',
  cancelled: 'bg-status-cancelled text-white dark:text-slate-950',
}

export const statusTextClassName: Record<Status, string> = {
  running: 'text-status-running',
  pending: 'text-status-pending',
  completed: 'text-status-completed',
  failed: 'text-status-failed',
  cancelled: 'text-status-cancelled',
}

// Lucide icon mapping. ShieldAlert=critical(중대 보안), AlertTriangle=high(주의),
// AlertCircle=medium(일반 경고), TrendingDown=low(경미), Info=info(정보성).
export const severityIcon: Record<Severity, LucideIcon> = {
  critical: ShieldAlert,
  high: AlertTriangle,
  medium: AlertCircle,
  low: TrendingDown,
  info: Info,
}

// Loader2=running(spinning), Clock=pending(대기), CheckCircle2=completed,
// XCircle=failed, ShieldOff=cancelled(중단).
export const statusIcon: Record<Status, LucideIcon> = {
  running: Loader2,
  pending: Clock,
  completed: CheckCircle2,
  failed: XCircle,
  cancelled: ShieldOff,
}

// i18n key — Stage 2 component가 t(severityLabel[level]) 형식으로 사용.
// dict.ts ko/en 양쪽에 severity.{level}, status.{level} 키가 존재해야 함.
export const severityLabel: Record<Severity, DictKey> = {
  critical: 'severity.critical',
  high: 'severity.high',
  medium: 'severity.medium',
  low: 'severity.low',
  info: 'severity.info',
}

export const statusLabel: Record<Status, DictKey> = {
  running: 'status.running',
  pending: 'status.pending',
  completed: 'status.completed',
  failed: 'status.failed',
  cancelled: 'status.cancelled',
}
