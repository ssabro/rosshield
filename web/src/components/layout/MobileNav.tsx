import * as DialogPrimitive from '@radix-ui/react-dialog'
import { Link } from '@tanstack/react-router'
import { Menu, ShieldCheck, X } from 'lucide-react'
import { useState } from 'react'

import { useHasPermission } from '@/api/hooks'
import { Button } from '@/components/ui/button'
import { useT } from '@/i18n/t'

import { filterVisibleGroups, NAV_GROUPS } from './nav-items'
import { SidebarNav } from './SidebarNav'

// D-UI-1 Stage 3 — 모바일 hamburger → 좌측 Sheet drawer.
//
// shadcn에 Sheet 컴포넌트는 없지만 radix Dialog Primitive로 직접 좌측 슬라이드
// 구성. vaul 등 추가 dep 없이 ~30줄로 충분.
//   - 트리거: md 미만에서만 표시되는 ghost 버튼 (header 좌측).
//   - drawer: 화면 좌측 60% (max 18rem) + 같은 NAV_GROUPS 렌더.
//   - 항목 click 시 자동 close — onNavigate로 SidebarNav에 전달.
//   - Esc·overlay click·X 버튼으로 닫힘 (Dialog 기본).

export function MobileNav(): React.ReactElement {
  const t = useT()
  const [open, setOpen] = useState(false)
  const canTenantAdmin = useHasPermission('tenant_admin', 'admin')
  const canSystemRead = useHasPermission('system', 'read')
  const visibleGroups = filterVisibleGroups(NAV_GROUPS, {
    canTenantAdmin,
    canSystemRead,
  })

  return (
    <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
      <DialogPrimitive.Trigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="h-9 w-9 px-0 md:hidden"
          aria-label={t('nav.menu.open')}
        >
          <Menu className="h-5 w-5" aria-hidden />
        </Button>
      </DialogPrimitive.Trigger>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/60 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
        <DialogPrimitive.Content
          aria-describedby={undefined}
          className="fixed left-0 top-0 z-50 flex h-full w-[min(18rem,80vw)] flex-col border-r border-border bg-card shadow-lg data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:slide-out-to-left data-[state=open]:slide-in-from-left"
        >
          <DialogPrimitive.Title className="sr-only">
            {t('app.brand')}
          </DialogPrimitive.Title>
          <div className="flex h-14 items-center justify-between gap-2 border-b border-border px-4">
            <Link
              to="/overview"
              className="flex items-center gap-2"
              onClick={() => setOpen(false)}
              aria-label={t('app.brand')}
            >
              <div className="rounded-md bg-primary/10 p-1.5">
                <ShieldCheck className="h-5 w-5 text-primary" aria-hidden />
              </div>
              <div className="flex flex-col leading-tight">
                <span className="text-sm font-semibold tracking-tight">
                  {t('app.brand')}
                </span>
                <span className="text-[10px] text-muted-foreground">
                  {t('app.brand.subtitle')}
                </span>
              </div>
            </Link>
            <DialogPrimitive.Close asChild>
              <Button
                variant="ghost"
                size="sm"
                className="h-8 w-8 px-0"
                aria-label={t('nav.menu.close')}
              >
                <X className="h-4 w-4" aria-hidden />
              </Button>
            </DialogPrimitive.Close>
          </div>
          <div className="flex-1 overflow-y-auto">
            <SidebarNav
              groups={visibleGroups}
              onNavigate={() => setOpen(false)}
            />
          </div>
          <div className="border-t border-border px-4 py-3 text-[10px] text-muted-foreground">
            {t('app.version')}
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
