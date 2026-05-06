import { type LucideIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

// EmptyState — 페이지/테이블의 "결과 없음" 상태를 통일된 시각으로 표현.
//
// 사용처: findings/compliance/advisor/scans 등에서 데이터 0건일 때.
// shadcn 다른 컴포넌트와 동일하게 Tailwind tokens만 사용 (theme 호환).
export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  className,
}: {
  icon?: LucideIcon
  title: string
  description?: string
  action?: React.ReactNode
  className?: string
}): React.ReactElement {
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center gap-3 rounded-md border border-dashed border-border bg-muted/30 px-6 py-10 text-center',
        className,
      )}
    >
      {Icon && (
        <div className="rounded-full bg-muted p-3 text-muted-foreground">
          <Icon className="h-6 w-6" aria-hidden />
        </div>
      )}
      <div className="space-y-1">
        <p className="text-sm font-medium text-foreground">{title}</p>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
      {action && <div className="pt-1">{action}</div>}
    </div>
  )
}
