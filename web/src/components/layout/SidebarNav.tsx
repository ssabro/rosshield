import { Link } from '@tanstack/react-router'

import { useT } from '@/i18n/t'
import { cn } from '@/lib/utils'

import type { NavGroup } from './nav-items'

// D-UI-1 Stage 3 — Sidebar 데스크탑 + Mobile drawer가 공유하는 nav 렌더러.
//
// 그룹 헤더는 uppercase 작은 라벨 (text-xs muted), 그 아래 항목 셋.
// 각 항목은 아이콘 + 라벨 + 옵션 badge ("opt-in"). active 라우트는 좌측 indicator bar.
//
// onNavigate: 모바일에서 항목 click 시 drawer를 close하는 콜백 (옵션).

export function SidebarNav({
  groups,
  onNavigate,
}: {
  groups: ReadonlyArray<NavGroup>
  onNavigate?: () => void
}): React.ReactElement {
  const t = useT()
  return (
    <nav aria-label={t('app.brand')} className="flex flex-col gap-4 p-3">
      {groups.map((group) => (
        <div key={group.labelKey} className="flex flex-col gap-0.5">
          <div className="px-3 pb-1.5 pt-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            {t(group.labelKey)}
          </div>
          {group.items.map((item) => (
            <Link
              key={item.to}
              to={item.to}
              activeProps={{
                className:
                  'bg-accent text-accent-foreground border-l-2 border-l-primary pl-[10px]',
              }}
              className={cn(
                'group flex items-center gap-2 rounded-md border-l-2 border-l-transparent py-2 pl-3 pr-3 text-sm transition-colors',
                'text-muted-foreground hover:bg-accent/60 hover:text-foreground',
              )}
              onClick={onNavigate}
            >
              <item.icon className="h-4 w-4" aria-hidden />
              <span>{t(item.labelKey)}</span>
              {item.badgeKey && (
                <span className="ml-auto rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                  {t(item.badgeKey)}
                </span>
              )}
            </Link>
          ))}
        </div>
      ))}
    </nav>
  )
}
