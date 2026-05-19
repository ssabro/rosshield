import { Link } from '@tanstack/react-router'
import { ShieldCheck } from 'lucide-react'

import { useHasPermission } from '@/api/hooks'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useT } from '@/i18n/t'

import { filterVisibleGroups, NAV_GROUPS } from './nav-items'
import { SidebarNav } from './SidebarNav'

// 좌측 사이드바 — IA 4 그룹 (`docs/design/notes/ui-review-ux.md` §4) + 하단 빌드 버전.
// `_authenticated` 셸 안에서만 렌더링된다.
//
// D-UI-1 Stage 3 변경:
//   - 데이터/렌더 분리: NAV_GROUPS(data) + SidebarNav(view).
//   - 그룹 헤더(text-xs uppercase) + advisor "opt-in" 보조 라벨.
//   - 모바일은 별 MobileNav 컴포넌트가 같은 그룹을 Sheet drawer로 제공.
//   - 데스크탑 사이드바는 md 이상 노출 (mobile은 hidden).
//
// 디자인 노트 (1차 폴리시 보존):
//   - 브랜드 영역에 작은 부제 ("Security Console") 추가
//   - 활성 메뉴는 좌측 indicator bar + 강조 배경 + 아이콘 색
//   - 로그아웃은 헤더로 이동(중복 방지) — 본 사이드바는 메뉴+브랜드만
//
// RBAC Stage 5 — 메뉴 가시성을 role 단위에서 permission 단위로 진화 (design doc §7).
//   매트릭스는 `nav-items.ts::filterVisibleGroups`에 위임 — Sidebar는 hook 결과만
//   넘긴다. 보안 경계는 server에서 RequirePermission middleware (Stage 4)로 강제.
export function Sidebar(): React.ReactElement {
  const t = useT()
  const canTenantAdmin = useHasPermission('tenant_admin', 'admin')
  const canSystemRead = useHasPermission('system', 'read')
  const visibleGroups = filterVisibleGroups(NAV_GROUPS, {
    canTenantAdmin,
    canSystemRead,
  })

  // 로그아웃은 Header에 통합 — 본 사이드바는 메뉴+브랜드 전용.
  // mobile은 hidden — MobileNav가 hamburger sheet로 같은 그룹을 노출.
  return (
    <aside className="hidden w-60 flex-col border-r border-border bg-card md:flex">
      <div className="flex h-14 items-center gap-2.5 border-b border-border px-4">
        <div className="rounded-md bg-primary/10 p-1.5">
          <ShieldCheck className="h-5 w-5 text-primary" aria-hidden />
        </div>
        <Link
          to="/overview"
          className="flex flex-col leading-tight"
          aria-label={t('app.brand')}
        >
          <span className="text-sm font-semibold tracking-tight">
            {t('app.brand')}
          </span>
          <span className="text-[10px] text-muted-foreground">
            {t('app.brand.subtitle')}
          </span>
        </Link>
      </div>

      <ScrollArea className="flex-1">
        <SidebarNav groups={visibleGroups} />
      </ScrollArea>

      <div className="border-t border-border px-4 py-3 text-[10px] text-muted-foreground">
        {t('app.version')}
      </div>
    </aside>
  )
}
