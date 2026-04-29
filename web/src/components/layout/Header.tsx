import { useNavigate } from '@tanstack/react-router'
import { LogOut } from 'lucide-react'

import { useMe } from '@/api/hooks'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth'

// 상단 헤더 — 우측에 사용자 이메일 + 로그아웃.
// - useMe로 fresh 데이터 시도하되, 캐시(useAuthStore)를 fallback.
// - Sidebar 브랜드와 중복 안 되도록 헤더에는 페이지 컨텍스트(현재 비움) + 사용자만.
export function Header(): React.ReactElement {
  const navigate = useNavigate()
  const storeUser = useAuthStore((s) => s.user)
  const clearSession = useAuthStore((s) => s.clearSession)
  const me = useMe()

  const email = me.data?.email ?? storeUser?.email ?? ''

  const handleLogout = (): void => {
    clearSession()
    void navigate({ to: '/login' })
  }

  return (
    <header className="flex h-14 items-center justify-end gap-3 border-b border-border bg-card px-6">
      {email && (
        <span className="text-sm text-muted-foreground" aria-label="현재 사용자">
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
    </header>
  )
}
