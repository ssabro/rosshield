import { useNavigate, useRouterState } from '@tanstack/react-router'
import { LogOut } from 'lucide-react'

import { useMe } from '@/api/hooks'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth'

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
