import { Link } from '@tanstack/react-router'
import { ChevronRight } from 'lucide-react'

import type { LinkProps } from '@tanstack/react-router'

// Breadcrumbs — drill-down 페이지 위에 표시되는 경로 표시.
//
// items 배열 — 각 항목은 label + 옵션 to(navigation). 마지막 항목은 보통 to 없음 (현재 페이지).
//
// 디자인:
//   - 작은 글자 (text-xs), muted color
//   - separator: lucide ChevronRight 14px
//   - 호버 가능한 링크는 underline
//   - 마지막 항목은 foreground color (현재 위치 강조)
//
// D-UI-1 Stage 3 — 본 컴포넌트는 이미 다음 drill-down 라우트에서 일관 사용 중:
//   - robots/$robotId, fleets/$fleetId, packs/$packKey, packs/$packKey/checks/$checkId
//   layout에서 자동 매핑(routeContext 기반)은 Stage 4 페이지 정비와 묶어 처리 —
//   본 Stage는 layout만 손대 페이지 회귀 0 목표.

export interface BreadcrumbItem {
  label: string
  to?: LinkProps['to']
  params?: Record<string, string>
}

export function Breadcrumbs({
  items,
}: {
  items: ReadonlyArray<BreadcrumbItem>
}): React.ReactElement {
  return (
    <nav className="flex items-center gap-1 text-xs text-muted-foreground" aria-label="Breadcrumb">
      {items.map((item, i) => {
        const isLast = i === items.length - 1
        const sep = i > 0 && (
          <ChevronRight aria-hidden="true" className="h-3 w-3 shrink-0" />
        )
        const node =
          item.to && !isLast ? (
            <Link
              to={item.to}
              params={item.params}
              className="hover:text-foreground hover:underline"
            >
              {item.label}
            </Link>
          ) : (
            <span className={isLast ? 'font-medium text-foreground' : ''}>
              {item.label}
            </span>
          )
        return (
          <span key={i} className="flex items-center gap-1">
            {sep}
            {node}
          </span>
        )
      })}
    </nav>
  )
}
