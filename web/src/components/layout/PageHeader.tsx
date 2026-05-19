import { cn } from '@/lib/utils'

// D-UI-1 Stage 2 — PageHeader 보강.
//
// 기존 (3 prop: title/description/actions/className) 호환 유지 + slot 추가:
//   - badge: title 옆 환경/상태 뱃지 (예: "Production", "Beta")
//   - breadcrumbs: title 위 경로 (Breadcrumbs 컴포넌트 직접 주입)
//
// 모든 _authenticated/* 페이지의 상단 타이틀 영역 공통 컴포넌트.
// visual hierarchy: breadcrumbs(작은 회색) → title+badge(큰 굵은) → description(보통 회색).

interface PageHeaderProps {
  title: string
  description?: string
  actions?: React.ReactNode
  className?: string
  badge?: React.ReactNode
  breadcrumbs?: React.ReactNode
}

export function PageHeader({
  title,
  description,
  actions,
  className,
  badge,
  breadcrumbs,
}: PageHeaderProps): React.ReactElement {
  return (
    <div className={cn('space-y-2', className)}>
      {breadcrumbs && <div className="text-xs">{breadcrumbs}</div>}
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
            {badge && <div className="flex items-center gap-1">{badge}</div>}
          </div>
          {description && (
            <p className="text-sm text-muted-foreground">{description}</p>
          )}
        </div>
        {actions && (
          <div className="flex shrink-0 items-center gap-2">{actions}</div>
        )}
      </div>
    </div>
  )
}
