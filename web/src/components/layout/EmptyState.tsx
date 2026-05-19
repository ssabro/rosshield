import {
  AlertTriangle,
  Inbox,
  Lock,
  SearchX,
  type LucideIcon,
} from 'lucide-react'

import { cn } from '@/lib/utils'

// EmptyState — 페이지/테이블의 "결과 없음" 상태를 통일된 시각으로 표현.
//
// D-UI-1 Stage 2 보강 — variant + size + breakdown 추가. 기존 호출지(11곳)와
// 호환을 위해 모든 prop은 optional 유지 (icon/title/description/action/className).
//
// variant 사용 가이드:
//   - `no-data` (default): 데이터 0건. CTA(예: "추가") 슬롯과 같이 사용.
//   - `no-permission`: RBAC 차단. 권한 안내 메시지에 적합.
//   - `loading-fail`: API 실패 → retry CTA 슬롯과 함께.
//   - `search-no-result`: 검색/필터 결과 0건. 필터 초기화 CTA 권장.
//
// 사용처: findings/compliance/advisor/scans/users/... 모든 list/table.
// shadcn 다른 컴포넌트와 동일하게 Tailwind tokens만 사용 (테마 호환).

export type EmptyStateVariant =
  | 'no-data'
  | 'no-permission'
  | 'loading-fail'
  | 'search-no-result'

export type EmptyStateSize = 'sm' | 'md' | 'lg'

interface EmptyStateProps {
  icon?: LucideIcon
  title: string
  description?: string
  action?: React.ReactNode
  className?: string
  variant?: EmptyStateVariant
  size?: EmptyStateSize
}

const VARIANT_DEFAULTS: Record<
  EmptyStateVariant,
  { icon: LucideIcon; tone: string }
> = {
  'no-data': { icon: Inbox, tone: 'text-muted-foreground' },
  'no-permission': { icon: Lock, tone: 'text-muted-foreground' },
  'loading-fail': { icon: AlertTriangle, tone: 'text-destructive' },
  'search-no-result': { icon: SearchX, tone: 'text-muted-foreground' },
}

const SIZE_CLASSES: Record<
  EmptyStateSize,
  {
    container: string
    iconWrap: string
    iconSize: string
    title: string
    description: string
  }
> = {
  sm: {
    container: 'px-4 py-6 gap-2',
    iconWrap: 'p-2',
    iconSize: 'h-4 w-4',
    title: 'text-sm',
    description: 'text-xs',
  },
  md: {
    container: 'px-6 py-10 gap-3',
    iconWrap: 'p-3',
    iconSize: 'h-6 w-6',
    title: 'text-sm',
    description: 'text-xs',
  },
  lg: {
    container: 'px-8 py-16 gap-4',
    iconWrap: 'p-4',
    iconSize: 'h-8 w-8',
    title: 'text-base',
    description: 'text-sm',
  },
}

export function EmptyState({
  icon,
  title,
  description,
  action,
  className,
  variant = 'no-data',
  size = 'md',
}: EmptyStateProps): React.ReactElement {
  const variantCfg = VARIANT_DEFAULTS[variant]
  const Icon = icon ?? variantCfg.icon
  const sizeCfg = SIZE_CLASSES[size]

  return (
    <div
      role="status"
      className={cn(
        'flex flex-col items-center justify-center rounded-md border border-dashed border-border bg-muted/30 text-center',
        sizeCfg.container,
        className,
      )}
    >
      {Icon && (
        <div className={cn('rounded-full bg-muted', sizeCfg.iconWrap, variantCfg.tone)}>
          <Icon className={sizeCfg.iconSize} aria-hidden />
        </div>
      )}
      <div className="space-y-1">
        <p className={cn('font-medium text-foreground', sizeCfg.title)}>{title}</p>
        {description && (
          <p className={cn('text-muted-foreground', sizeCfg.description)}>
            {description}
          </p>
        )}
      </div>
      {action && <div className="pt-1">{action}</div>}
    </div>
  )
}
