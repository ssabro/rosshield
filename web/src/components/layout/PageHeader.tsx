import { cn } from '@/lib/utils'

// 폴리시 2차 — 모든 _authenticated/* 페이지의 상단 타이틀 영역 공통 컴포넌트.
// 기존 패턴(<h1 text-2xl font-semibold> + <p text-sm muted-foreground>)을 그대로 보존.

export function PageHeader({
  title,
  description,
  actions,
  className,
}: {
  title: string
  description?: string
  actions?: React.ReactNode
  className?: string
}): React.ReactElement {
  return (
    <div className={cn('flex items-start justify-between gap-4', className)}>
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {description && (
          <p className="text-sm text-muted-foreground">{description}</p>
        )}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  )
}
