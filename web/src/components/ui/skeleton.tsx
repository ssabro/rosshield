import { cn } from '@/lib/utils'

// D-UI-1 Stage 2 — Skeleton (shadcn 표준) + 변형 (Table/Card/Text/Page).
//
// 사용처: 페이지 로딩 중 layout shift 방지. spinner 대비 perceived performance↑.
// animate-pulse + bg-muted Tailwind class만 사용 (테마 호환).

export function Skeleton({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>): React.ReactElement {
  return (
    <div
      className={cn('animate-pulse rounded-md bg-muted', className)}
      aria-hidden
      {...props}
    />
  )
}

// 텍스트 한 줄 placeholder. 폭은 props로 조정 (Tailwind w-*).
export function TextSkeleton({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>): React.ReactElement {
  return <Skeleton className={cn('h-4 w-full', className)} {...props} />
}

// 테이블 row 여러 줄 placeholder.
// 첫 column은 약간 좁게, 마지막 column은 더 좁게 — 실제 데이터 폭 유추.
export function TableRowSkeleton({
  rows = 5,
  columns = 4,
  className,
}: {
  rows?: number
  columns?: number
  className?: string
}): React.ReactElement {
  return (
    <div className={cn('space-y-2', className)} role="status" aria-label="불러오는 중">
      {Array.from({ length: rows }).map((_, rowIdx) => (
        <div key={rowIdx} className="flex items-center gap-3">
          {Array.from({ length: columns }).map((__, colIdx) => (
            <Skeleton
              key={colIdx}
              className={cn(
                'h-4',
                colIdx === 0 && 'w-1/6',
                colIdx > 0 && colIdx < columns - 1 && 'flex-1',
                colIdx === columns - 1 && 'w-1/12',
              )}
            />
          ))}
        </div>
      ))}
    </div>
  )
}

// 카드 1개 placeholder — title + description + body 3줄 구조.
export function CardSkeleton({
  className,
}: {
  className?: string
}): React.ReactElement {
  return (
    <div
      className={cn('rounded-lg border bg-card p-4 shadow-sm', className)}
      role="status"
      aria-label="불러오는 중"
    >
      <div className="space-y-3">
        <Skeleton className="h-5 w-1/3" />
        <Skeleton className="h-3 w-2/3" />
        <div className="space-y-2 pt-2">
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-5/6" />
          <Skeleton className="h-3 w-3/4" />
        </div>
      </div>
    </div>
  )
}

// 페이지 전체 placeholder — 헤더 + 본문 row 6줄.
export function PageSkeleton({
  className,
}: {
  className?: string
}): React.ReactElement {
  return (
    <div className={cn('space-y-6', className)} role="status" aria-label="페이지 불러오는 중">
      <div className="space-y-2">
        <Skeleton className="h-7 w-1/3" />
        <Skeleton className="h-4 w-1/2" />
      </div>
      <TableRowSkeleton rows={6} columns={4} />
    </div>
  )
}
