import { useNavigate, useRouterState } from '@tanstack/react-router'
import { LogOut, Monitor, Moon, Sun } from 'lucide-react'

import { useMe } from '@/api/hooks'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore, type Theme } from '@/stores/theme'

// 상단 헤더 — 좌측 페이지 컨텍스트(현재 라우트 라벨) + 우측 사용자 이메일 + 로그아웃.
//
// 라우트별 한글 타이틀은 PAGE_TITLES 맵으로 관리. Sidebar의 메뉴 라벨과 일치시킴.
const PAGE_TITLES: Record<string, string> = {
  '/robots': '로봇',
  '/scans': '스캔',
  '/findings': 'Findings',
  '/compliance': 'Compliance',
  '/advisor': 'Advisor',
  '/reports': '리포트',
}

export function Header(): React.ReactElement {
  const navigate = useNavigate()
  const storeUser = useAuthStore((s) => s.user)
  const clearSession = useAuthStore((s) => s.clearSession)
  const me = useMe()
  const matches = useRouterState({ select: (s) => s.matches })
  const theme = useThemeStore((s) => s.theme)
  const setTheme = useThemeStore((s) => s.setTheme)

  const email = me.data?.email ?? storeUser?.email ?? ''
  const pathname = matches[matches.length - 1]?.pathname ?? '/'
  const title = PAGE_TITLES[pathname] ?? ''

  const handleLogout = (): void => {
    clearSession()
    void navigate({ to: '/login' })
  }

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
            aria-label="현재 사용자"
            title={email}
          >
            {email}
          </span>
        )}
        <Button
          variant="ghost"
          size="sm"
          className="h-8 w-8 px-0"
          onClick={() => setTheme(nextTheme(theme))}
          aria-label={`테마 (${themeLabel(theme)})`}
          title={`테마: ${themeLabel(theme)} (클릭으로 전환)`}
        >
          <ThemeIcon theme={theme} />
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className="gap-2"
          onClick={handleLogout}
          aria-label="로그아웃"
        >
          <LogOut className="h-4 w-4" aria-hidden />
          로그아웃
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

function themeLabel(theme: Theme): string {
  if (theme === 'light') return '라이트'
  if (theme === 'dark') return '다크'
  return '시스템'
}

function ThemeIcon({ theme }: { theme: Theme }): React.ReactElement {
  if (theme === 'light') return <Sun className="h-4 w-4" aria-hidden />
  if (theme === 'dark') return <Moon className="h-4 w-4" aria-hidden />
  return <Monitor className="h-4 w-4" aria-hidden />
}
