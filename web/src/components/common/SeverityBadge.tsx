import { cn } from '@/lib/utils'
import {
  severityClassName,
  severityIcon,
  severityLabel,
  type Severity,
} from '@/lib/severity'

// D-UI-1 Stage 2 — SeverityBadge.
//
// a11y review P0 — 색만으로 정보 전달 금지(WCAG 2.2 1.4.1). 본 컴포넌트는
// (1) icon + (2) text label + (3) color 3중 채널로 severity를 전달한다.
//
// 사용처: findings/checks/scans 등 severity column. 기존 `<Badge variant="...">`
// 임시 매핑을 통일하여 토큰/라벨/아이콘 분기를 한 곳에 모은다.
//
// import { SeverityBadge } from '@/components/common/SeverityBadge'
// <SeverityBadge severity={check.severity} />

export type SeverityBadgeSize = 'sm' | 'md'

interface SeverityBadgeProps {
  severity: Severity
  showIcon?: boolean
  size?: SeverityBadgeSize
  className?: string
}

const SIZE_CLASSES: Record<SeverityBadgeSize, { wrapper: string; icon: string }> = {
  sm: { wrapper: 'px-1.5 py-0.5 text-[10px]', icon: 'size-2.5' },
  md: { wrapper: 'px-2 py-0.5 text-xs', icon: 'size-3' },
}

export function SeverityBadge({
  severity,
  showIcon = true,
  size = 'md',
  className,
}: SeverityBadgeProps): React.ReactElement {
  const Icon = severityIcon[severity]
  const sizeCfg = SIZE_CLASSES[size]
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded font-medium',
        sizeCfg.wrapper,
        severityClassName[severity],
        className,
      )}
      data-severity={severity}
    >
      {showIcon && <Icon className={sizeCfg.icon} aria-hidden />}
      {severityLabel[severity]}
    </span>
  )
}
