import { Link } from '@tanstack/react-router'
import {
  AlertTriangle,
  Award,
  ClipboardCheck,
  FileText,
  KeyRound,
  LayoutDashboard,
  MessageSquare,
  PlayCircle,
  ScrollText,
  Server,
  Settings as SettingsIcon,
  ShieldCheck,
  Webhook,
} from 'lucide-react'

import { ScrollArea } from '@/components/ui/scroll-area'
import { useT } from '@/i18n/t'
import { cn } from '@/lib/utils'

import type { DictKey } from '@/i18n/dict'

// 좌측 사이드바 — 6 메뉴 + 하단 빌드 버전(브랜드 영역).
// `_authenticated` 셸 안에서만 렌더링된다.
//
// 디자인 노트 (1차 폴리시):
//   - 브랜드 영역에 작은 부제 ("Security Console") 추가
//   - 활성 메뉴는 좌측 indicator bar + 강조 배경 + 아이콘 색
//   - 로그아웃은 헤더로 이동(중복 방지) — 본 사이드바는 메뉴+브랜드만
const items: ReadonlyArray<{
  to:
    | '/overview'
    | '/robots'
    | '/scans'
    | '/findings'
    | '/compliance'
    | '/advisor'
    | '/reports'
    | '/audit'
    | '/integrations'
    | '/sso'
    | '/license'
    | '/settings'
  labelKey: DictKey
  icon: typeof Server
}> = [
  { to: '/overview', labelKey: 'nav.overview', icon: LayoutDashboard },
  { to: '/robots', labelKey: 'nav.robots', icon: Server },
  { to: '/scans', labelKey: 'nav.scans', icon: PlayCircle },
  { to: '/findings', labelKey: 'nav.findings', icon: AlertTriangle },
  { to: '/compliance', labelKey: 'nav.compliance', icon: ClipboardCheck },
  { to: '/advisor', labelKey: 'nav.advisor', icon: MessageSquare },
  { to: '/reports', labelKey: 'nav.reports', icon: FileText },
  { to: '/audit', labelKey: 'nav.audit', icon: ScrollText },
  { to: '/integrations', labelKey: 'nav.integrations', icon: Webhook },
  { to: '/sso', labelKey: 'nav.sso', icon: KeyRound },
  { to: '/license', labelKey: 'nav.license', icon: Award },
  { to: '/settings', labelKey: 'nav.settings', icon: SettingsIcon },
]

export function Sidebar(): React.ReactElement {
  const t = useT()
  // 로그아웃은 Header에 통합 — 본 사이드바는 메뉴+브랜드 전용.
  return (
    <aside className="flex w-60 flex-col border-r border-border bg-card">
      <div className="flex h-14 items-center gap-2.5 border-b border-border px-4">
        <div className="rounded-md bg-primary/10 p-1.5">
          <ShieldCheck className="h-5 w-5 text-primary" aria-hidden />
        </div>
        <div className="flex flex-col leading-tight">
          <span className="text-sm font-semibold tracking-tight">{t('app.brand')}</span>
          <span className="text-[10px] text-muted-foreground">
            {t('app.brand.subtitle')}
          </span>
        </div>
      </div>

      <ScrollArea className="flex-1">
        <nav aria-label={t('app.brand')} className="flex flex-col gap-0.5 p-3">
          {items.map((item) => (
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
            >
              <item.icon className="h-4 w-4" aria-hidden />
              <span>{t(item.labelKey)}</span>
            </Link>
          ))}
        </nav>
      </ScrollArea>

      <div className="border-t border-border px-4 py-3 text-[10px] text-muted-foreground">
        {t('app.version')}
      </div>
    </aside>
  )
}
