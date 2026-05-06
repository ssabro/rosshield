import { Link, useNavigate } from '@tanstack/react-router'
import {
  AlertTriangle,
  ClipboardCheck,
  FileText,
  LogOut,
  MessageSquare,
  PlayCircle,
  Server,
  ShieldCheck,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'

// 좌측 사이드바 — 4 메뉴(개요는 Phase 2) + 로그아웃.
// `_authenticated` 셸 안에서만 렌더링된다.
const items = [
  { to: '/robots', label: '로봇', icon: Server },
  { to: '/scans', label: '스캔', icon: PlayCircle },
  { to: '/findings', label: 'Findings', icon: AlertTriangle },
  { to: '/compliance', label: 'Compliance', icon: ClipboardCheck },
  { to: '/advisor', label: 'Advisor', icon: MessageSquare },
  { to: '/reports', label: '리포트', icon: FileText },
] as const

export function Sidebar(): React.ReactElement {
  const navigate = useNavigate()
  const clearSession = useAuthStore((s) => s.clearSession)

  const handleLogout = (): void => {
    clearSession()
    void navigate({ to: '/login' })
  }

  return (
    <aside className="flex w-56 flex-col border-r border-border bg-card">
      <div className="flex h-14 items-center gap-2 border-b border-border px-4">
        <ShieldCheck className="h-5 w-5 text-primary" aria-hidden />
        <span className="font-semibold tracking-tight">rosshield</span>
      </div>

      <ScrollArea className="flex-1">
        <nav aria-label="주 메뉴" className="flex flex-col gap-1 p-3">
          {items.map((item) => (
            <Link
              key={item.to}
              to={item.to}
              activeProps={{ className: 'bg-accent text-accent-foreground' }}
              className={cn(
                'flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors',
                'text-muted-foreground hover:bg-accent hover:text-accent-foreground',
              )}
            >
              <item.icon className="h-4 w-4" aria-hidden />
              <span>{item.label}</span>
            </Link>
          ))}
        </nav>
      </ScrollArea>

      <div className="border-t border-border p-3">
        <Button
          variant="ghost"
          size="sm"
          className="w-full justify-start gap-2 text-muted-foreground"
          onClick={handleLogout}
        >
          <LogOut className="h-4 w-4" aria-hidden />
          로그아웃
        </Button>
      </div>
    </aside>
  )
}
