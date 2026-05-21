import { Link, useRouterState } from '@tanstack/react-router'
import { Search } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { useT } from '@/i18n/t'

import { LocaleToggle } from './LocaleToggle'
import { MobileNav } from './MobileNav'
import { TenantRoleBadge } from './TenantRoleBadge'
import { ThemeToggle, nextTheme as themeNext, themeLabelKey as themeLabel } from './ThemeToggle'
import { UserMenu } from './UserMenu'

import type { DictKey } from '@/i18n/dict'

// D-UI-1 Stage 3 — Header 전면 재구성 (좌→우):
//   (mobile) hamburger | Lodestar logo | 페이지 타이틀 | spacer
//   Search(Ctrl+K placeholder) | LocaleToggle | ThemeToggle | TenantRoleBadge | UserMenu
//
// 각 위젯은 layout/*.tsx로 분리 — Header.tsx는 조립만 담당해 ≤ 100줄 유지.
//
// 회귀 호환: Header.tsx에서 `nextTheme`/`themeLabelKey`를 ThemeToggle로 위임하지만
// Header.test.tsx가 `import { nextTheme } from './Header'`를 사용하므로 re-export.

const PAGE_TITLE_KEYS: Record<string, DictKey> = {
  '/overview': 'nav.overview',
  '/fleets': 'nav.fleets',
  '/robots': 'nav.robots',
  '/scans': 'nav.scans',
  '/findings': 'nav.findings',
  '/compliance': 'nav.compliance',
  '/advisor': 'nav.advisor',
  '/reports': 'nav.reports',
  '/audit': 'nav.audit',
  '/integrations': 'nav.integrations',
  '/sso': 'nav.sso',
  '/users': 'nav.users',
  '/license': 'nav.license',
  '/regions': 'nav.regions',
  '/system': 'nav.system',
  '/settings': 'nav.settings',
}

export function Header(): React.ReactElement {
  const matches = useRouterState({ select: (s) => s.matches })
  const t = useT()

  const pathname = matches[matches.length - 1]?.pathname ?? '/'
  const titleKey = PAGE_TITLE_KEYS[pathname]
  const title = titleKey ? t(titleKey) : ''

  return (
    <header className="flex h-14 items-center gap-3 border-b border-border bg-card px-4 md:px-6">
      <MobileNav />

      {/* 모바일 — sidebar 비표시 상태에서 brand 노출 (md 이하 only). */}
      <Link
        to="/overview"
        className="flex items-center md:hidden"
        aria-label={t('app.brand')}
      >
        <span className="text-sm font-semibold tracking-tight text-foreground">
          {t('app.brand')}
        </span>
      </Link>

      {title && (
        <h2 className="hidden truncate text-sm font-medium tracking-tight text-foreground md:block">
          {title}
        </h2>
      )}

      <div className="ml-auto flex items-center gap-1.5 sm:gap-2">
        {/* Search — Ctrl+K placeholder (E13 이후 Command palette 구현 예정). */}
        <Button
          variant="ghost"
          size="sm"
          className="hidden h-8 w-8 px-0 md:inline-flex"
          aria-label={t('header.search.aria')}
          title={t('header.search.placeholder')}
          disabled
        >
          <Search className="h-4 w-4" aria-hidden />
        </Button>
        <LocaleToggle />
        <ThemeToggle />
        <TenantRoleBadge />
        <UserMenu />
      </div>
    </header>
  )
}

// 회귀 호환 re-export (Header.test.tsx).
export const nextTheme = themeNext
export const themeLabelKey = themeLabel
