import { Link } from '@tanstack/react-router'
import {
  Activity,
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
  Users,
  Webhook,
} from 'lucide-react'

import { useHasPermission } from '@/api/hooks'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useT } from '@/i18n/t'
import { cn } from '@/lib/utils'

import type { DictKey } from '@/i18n/dict'
import type { Action, Resource } from '@/lib/authz/policy'

// 좌측 사이드바 — 메뉴 + 하단 빌드 버전.
// `_authenticated` 셸 안에서만 렌더링된다.
//
// 디자인 노트 (1차 폴리시):
//   - 브랜드 영역에 작은 부제 ("Security Console") 추가
//   - 활성 메뉴는 좌측 indicator bar + 강조 배경 + 아이콘 색
//   - 로그아웃은 헤더로 이동(중복 방지) — 본 사이드바는 메뉴+브랜드만
//
// RBAC Stage 5 — 메뉴 가시성을 role 단위에서 permission 단위로 진화 (design doc §7).
//   - requires {resource, action} → 해당 권한 보유 사용자만 메뉴 표시.
//   - requires 미설정             → 모든 인증 사용자에게 표시.
//
// 매핑 (Stage 2-D 호환 + permission 매트릭스 §3.3 일치):
//   - sso·users     : tenant_admin.admin (admin only — Stage 4 server gate와 동일)
//   - fleets·system : system.read (admin·auditor — Stage 2-A backup 매핑과 일관)
// fleet 단위 메뉴(fleet list 안)는 binding 보유 fleet만 표시 — Phase 6 fleet detail
// shell에서 별 처리.
//
// 보안 경계는 server에서 RequirePermission middleware (Stage 4)로 강제 — 본 필터는
// UX 정리.
type PermissionRequirement = { resource: Resource; action: Action }

const items: ReadonlyArray<{
  to:
    | '/overview'
    | '/fleets'
    | '/robots'
    | '/scans'
    | '/findings'
    | '/compliance'
    | '/advisor'
    | '/reports'
    | '/audit'
    | '/integrations'
    | '/sso'
    | '/users'
    | '/license'
    | '/system'
    | '/settings'
  labelKey: DictKey
  icon: typeof Server
  requires?: PermissionRequirement
}> = [
  { to: '/overview', labelKey: 'nav.overview', icon: LayoutDashboard },
  // fleets 메뉴 — admin/auditor만 (Stage 2-D 호환). system.read 매핑.
  { to: '/fleets', labelKey: 'nav.fleets', icon: ShieldCheck, requires: { resource: 'system', action: 'read' } },
  { to: '/robots', labelKey: 'nav.robots', icon: Server },
  { to: '/scans', labelKey: 'nav.scans', icon: PlayCircle },
  { to: '/findings', labelKey: 'nav.findings', icon: AlertTriangle },
  { to: '/compliance', labelKey: 'nav.compliance', icon: ClipboardCheck },
  { to: '/advisor', labelKey: 'nav.advisor', icon: MessageSquare },
  { to: '/reports', labelKey: 'nav.reports', icon: FileText },
  { to: '/audit', labelKey: 'nav.audit', icon: ScrollText },
  { to: '/integrations', labelKey: 'nav.integrations', icon: Webhook },
  // sso/users — admin 전용 (tenant_admin.admin).
  { to: '/sso', labelKey: 'nav.sso', icon: KeyRound, requires: { resource: 'tenant_admin', action: 'admin' } },
  { to: '/users', labelKey: 'nav.users', icon: Users, requires: { resource: 'tenant_admin', action: 'admin' } },
  { to: '/license', labelKey: 'nav.license', icon: Award },
  // system 운영 — admin/auditor (system.read).
  { to: '/system', labelKey: 'nav.system', icon: Activity, requires: { resource: 'system', action: 'read' } },
  { to: '/settings', labelKey: 'nav.settings', icon: SettingsIcon },
]

export function Sidebar(): React.ReactElement {
  const t = useT()
  // RBAC Stage 5 — permission 단위 평가 (design doc §7 Stage 5).
  //   tenant_admin.admin · system.read 두 셋만 평가 — 추가 권한 도입 시 본 hook 추가.
  //   server `RequirePermission` middleware와 동일한 매트릭스로 일관 (lib/authz/policy).
  const canTenantAdmin = useHasPermission('tenant_admin', 'admin')
  const canSystemRead = useHasPermission('system', 'read')

  // 메뉴 필터 — requires 매트릭스 매핑.
  const visibleItems = items.filter((item) => {
    if (!item.requires) return true
    const { resource, action } = item.requires
    if (resource === 'tenant_admin' && action === 'admin') return canTenantAdmin
    if (resource === 'system' && action === 'read') return canSystemRead
    // 매트릭스 외 — 안전 default false (보수적).
    return false
  })

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
          {visibleItems.map((item) => (
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
