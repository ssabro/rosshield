import { useNavigate, useRouterState } from '@tanstack/react-router'
import { Globe, LogOut, Monitor, Moon, Sun } from 'lucide-react'

import { useMe } from '@/api/hooks'
import { Button } from '@/components/ui/button'
import { useT } from '@/i18n/t'
import { nextLocale, useLocaleStore } from '@/i18n/store'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore, type Theme } from '@/stores/theme'

import type { DictKey, Locale } from '@/i18n/dict'

// 상단 헤더 — 좌측 페이지 컨텍스트(현재 라우트 라벨) + 우측 사용자 이메일/테마/언어/로그아웃.
//
// 라우트별 타이틀 키는 PAGE_TITLE_KEYS 맵으로 관리. Sidebar의 메뉴 라벨과 같은 dict 사용.
const PAGE_TITLE_KEYS: Record<string, DictKey> = {
  '/robots': 'nav.robots',
  '/scans': 'nav.scans',
  '/findings': 'nav.findings',
  '/compliance': 'nav.compliance',
  '/advisor': 'nav.advisor',
  '/reports': 'nav.reports',
}

export function Header(): React.ReactElement {
  const navigate = useNavigate()
  const storeUser = useAuthStore((s) => s.user)
  const clearSession = useAuthStore((s) => s.clearSession)
  const me = useMe()
  const matches = useRouterState({ select: (s) => s.matches })
  const theme = useThemeStore((s) => s.theme)
  const setTheme = useThemeStore((s) => s.setTheme)
  const locale = useLocaleStore((s) => s.locale)
  const setLocale = useLocaleStore((s) => s.setLocale)
  const t = useT()

  const email = me.data?.email ?? storeUser?.email ?? ''
  const pathname = matches[matches.length - 1]?.pathname ?? '/'
  const titleKey = PAGE_TITLE_KEYS[pathname]
  const title = titleKey ? t(titleKey) : ''

  const handleLogout = (): void => {
    clearSession()
    void navigate({ to: '/login' })
  }

  const themeLbl = t(themeLabelKey(theme))

  return (
    <header className="flex h-14 items-center gap-3 border-b border-border bg-card px-6">
      {title && (
        <h2 className="text-sm font-medium tracking-tight text-foreground">
          {title}
        </h2>
      )}
      <div className="ml-auto flex items-center gap-3">
        {email && (
          <span
            className="text-xs text-muted-foreground"
            aria-label={t('header.user.aria')}
            title={email}
          >
            {email}
          </span>
        )}
        <Button
          variant="ghost"
          size="sm"
          className="h-8 w-8 px-0"
          onClick={() => setLocale(nextLocale(locale))}
          aria-label={t('header.locale.aria', { label: localeLabel(locale) })}
          title={t('header.locale.tooltip', { label: localeLabel(locale) })}
        >
          <Globe className="h-4 w-4" aria-hidden />
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className="h-8 w-8 px-0"
          onClick={() => setTheme(nextTheme(theme))}
          aria-label={t('header.theme.aria', { label: themeLbl })}
          title={t('header.theme.tooltip', { label: themeLbl })}
        >
          <ThemeIcon theme={theme} />
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className="gap-2"
          onClick={handleLogout}
          aria-label={t('header.logout')}
        >
          <LogOut className="h-4 w-4" aria-hidden />
          {t('header.logout')}
        </Button>
      </div>
    </header>
  )
}

// nextTheme — 토글 순서: light → dark → system → light
export function nextTheme(theme: Theme): Theme {
  if (theme === 'light') return 'dark'
  if (theme === 'dark') return 'system'
  return 'light'
}

export function themeLabelKey(theme: Theme): DictKey {
  if (theme === 'light') return 'header.theme.light'
  if (theme === 'dark') return 'header.theme.dark'
  return 'header.theme.system'
}

function localeLabel(locale: Locale): string {
  return locale === 'ko' ? '한국어' : 'English'
}

function ThemeIcon({ theme }: { theme: Theme }): React.ReactElement {
  if (theme === 'light') return <Sun className="h-4 w-4" aria-hidden />
  if (theme === 'dark') return <Moon className="h-4 w-4" aria-hidden />
  return <Monitor className="h-4 w-4" aria-hidden />
}
