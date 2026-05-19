import { useNavigate } from '@tanstack/react-router'
import { LogOut, Settings as SettingsIcon, User as UserIcon } from 'lucide-react'

import { useLogout, useMe } from '@/api/hooks'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useT } from '@/i18n/t'
import { useAuthStore } from '@/stores/auth'

// D-UI-1 Stage 3 — Header 우측 사용자 메뉴 (avatar initial + dropdown).
//
// 표시:
//   - 트리거: 원형 avatar (이메일 첫 글자 대문자, primary 톤)
//   - dropdown:
//     · 이메일 (라벨)
//     · Settings 진입 (`/settings`)
//     · 로그아웃 (POST /auth/logout → /login redirect)
//
// 기존 Header 우측에 노출되던 email span + logout button을 본 메뉴로 통합.
// 우측 행이 정리되어 tenant·role badge가 잘 보이도록 한다.

export function UserMenu(): React.ReactElement {
  const t = useT()
  const navigate = useNavigate()
  const storeUser = useAuthStore((s) => s.user)
  const me = useMe()
  const logout = useLogout()
  const email = me.data?.email ?? storeUser?.email ?? ''
  const initial = (email.trim().charAt(0) || '?').toUpperCase()

  const handleLogout = async (): Promise<void> => {
    await logout.mutateAsync()
    void navigate({ to: '/login' })
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="h-9 w-9 rounded-full px-0"
          aria-label={t('header.user.menu.aria')}
          title={email || t('header.user.aria')}
        >
          {email ? (
            <span
              aria-hidden
              className="flex h-7 w-7 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary"
            >
              {initial}
            </span>
          ) : (
            <UserIcon className="h-4 w-4" aria-hidden />
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel className="flex flex-col gap-0.5 py-2">
          <span className="text-[10px] font-normal uppercase tracking-wider text-muted-foreground">
            {t('header.user.menu.signedInAs')}
          </span>
          <span className="truncate text-sm font-medium" title={email}>
            {email || '—'}
          </span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => void navigate({ to: '/settings' })}>
          <SettingsIcon className="mr-2 h-4 w-4" aria-hidden />
          {t('header.user.menu.profile')}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={() => void handleLogout()}
          disabled={logout.isPending}
        >
          <LogOut className="mr-2 h-4 w-4" aria-hidden />
          {t('header.logout')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
