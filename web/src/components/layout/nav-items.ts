import {
  Activity,
  AlertTriangle,
  Award,
  ClipboardCheck,
  FileText,
  Globe,
  KeyRound,
  LayoutDashboard,
  PlayCircle,
  ScrollText,
  Server,
  Settings as SettingsIcon,
  ShieldCheck,
  Sparkles,
  Users,
  Webhook,
} from 'lucide-react'

import type { DictKey } from '@/i18n/dict'
import type { Action, Resource } from '@/lib/authz/policy'

// D-UI-1 Stage 3 — 사이드바·모바일 drawer가 공유하는 navigation 데이터.
//
// IA 개편 (`docs/design/notes/ui-review-ux.md` §4): 15개 flat 메뉴를 JTBD 기반
// 4 그룹으로 재구성해 click 깊이/시각 부담을 줄임.
//   - 운영 (Operations): 일상 모니터 + 실행
//   - 컴플라이언스 (Compliance): 감사·보고
//   - 지능화 (Intelligence): AI 옵트인 (원칙 2)
//   - 관리 (Admin): 권한·시스템
//
// 권한 매트릭스는 기존 Sidebar.tsx와 동일 (RBAC Stage 5):
//   - sso·users : tenant_admin.admin
//   - fleets·system : system.read

export type PermissionRequirement = { resource: Resource; action: Action }

export interface NavItem {
  to:
    | '/overview'
    | '/fleets'
    | '/robots'
    | '/scans'
    | '/findings'
    | '/compliance'
    | '/compliance/export'
    | '/compliance/effectiveness'
    | '/advisor'
    | '/reports'
    | '/audit'
    | '/integrations'
    | '/sso'
    | '/users'
    | '/license'
    | '/regions'
    | '/system'
    | '/settings'
  labelKey: DictKey
  icon: typeof Server
  requires?: PermissionRequirement
  // 보조 텍스트 (예: advisor의 "opt-in"). 표시 시 muted/badge 스타일.
  badgeKey?: DictKey
}

export interface NavGroup {
  // 그룹 헤더 라벨 키 (예: 'nav.group.operations').
  labelKey: DictKey
  items: ReadonlyArray<NavItem>
}

// /packs는 사이드바 메뉴 항목으로 별도 노출되지 않음 (Stage 3 IA: 컴플라이언스
// 작업 흐름 안에서 진입 — Reports/Compliance에서 navigate). 추후 직접 메뉴 노출
// 결정 시 본 배열에 추가.
//
// /settings은 라우트 존재 (settings.tsx) — 사용자 헤더 메뉴에서 진입 + 사이드바
// 관리 그룹에도 노출 (light-weight).
export const NAV_GROUPS: ReadonlyArray<NavGroup> = [
  {
    labelKey: 'nav.group.operations',
    items: [
      { to: '/overview', labelKey: 'nav.overview', icon: LayoutDashboard },
      { to: '/scans', labelKey: 'nav.scans', icon: PlayCircle },
      { to: '/findings', labelKey: 'nav.findings', icon: AlertTriangle },
      { to: '/robots', labelKey: 'nav.robots', icon: Server },
      // fleets — admin/auditor만 (Stage 2-D 호환). system.read 매핑.
      {
        to: '/fleets',
        labelKey: 'nav.fleets',
        icon: ShieldCheck,
        requires: { resource: 'system', action: 'read' },
      },
    ],
  },
  {
    labelKey: 'nav.group.compliance',
    items: [
      { to: '/compliance', labelKey: 'nav.compliance', icon: ClipboardCheck },
      // audit log export wizard — admin + auditor (audit.export 권한).
      // Phase 11.B-5: nav 매트릭스는 tenant_admin.admin / system.read 두 분기만 cover.
      // auditor 는 system.read 보유(매트릭스 §3.3) — system.read 로 게이트하면 admin + auditor
      // 둘 다 visible. fleet-admin/operator/read-only 는 system.read 미보유로 자연 hide.
      {
        to: '/compliance/export',
        labelKey: 'nav.compliance.export',
        icon: FileText,
        requires: { resource: 'system', action: 'read' },
      },
      // Phase 11.B-6 — SOC2 effectiveness dashboard. 동일 audit.export 권한 게이트.
      // system.read 도 admin + auditor 만 통과 — 게이트 효과는 audit.export 와 일관.
      {
        to: '/compliance/effectiveness',
        labelKey: 'nav.compliance.effectiveness',
        icon: ShieldCheck,
        requires: { resource: 'system', action: 'read' },
      },
      { to: '/reports', labelKey: 'nav.reports', icon: FileText },
      { to: '/audit', labelKey: 'nav.audit', icon: ScrollText },
    ],
  },
  {
    labelKey: 'nav.group.intelligence',
    items: [
      // Advisor — 옵트인 (원칙 2). subtle badge "opt-in" 노출.
      {
        to: '/advisor',
        labelKey: 'nav.advisor',
        icon: Sparkles,
        badgeKey: 'nav.advisor.optIn',
      },
    ],
  },
  {
    labelKey: 'nav.group.admin',
    items: [
      { to: '/integrations', labelKey: 'nav.integrations', icon: Webhook },
      // sso/users — admin 전용 (tenant_admin.admin).
      {
        to: '/sso',
        labelKey: 'nav.sso',
        icon: KeyRound,
        requires: { resource: 'tenant_admin', action: 'admin' },
      },
      {
        to: '/users',
        labelKey: 'nav.users',
        icon: Users,
        requires: { resource: 'tenant_admin', action: 'admin' },
      },
      { to: '/license', labelKey: 'nav.license', icon: Award },
      // regions — Phase 10.A-2 multi-region UX 표면화. admin 전용
      // (handlers.go ListReplicas는 tenant_admin.admin 게이트).
      {
        to: '/regions',
        labelKey: 'nav.regions',
        icon: Globe,
        requires: { resource: 'tenant_admin', action: 'admin' },
      },
      // system 운영 — admin/auditor (system.read).
      {
        to: '/system',
        labelKey: 'nav.system',
        icon: Activity,
        requires: { resource: 'system', action: 'read' },
      },
      { to: '/settings', labelKey: 'nav.settings', icon: SettingsIcon },
    ],
  },
]

// useVisibleNavGroups — RBAC permission 매트릭스 적용 후 그룹 별 visible 메뉴 셋 반환.
// 그룹 안 메뉴가 0개면 그룹 자체도 hide.
// 추가 권한 도입 시 본 hook + Sidebar 매핑을 동시 갱신.
export function filterVisibleGroups(
  groups: ReadonlyArray<NavGroup>,
  perms: {
    canTenantAdmin: boolean
    canSystemRead: boolean
  },
): ReadonlyArray<NavGroup> {
  return groups
    .map((g) => ({
      labelKey: g.labelKey,
      items: g.items.filter((item) => {
        if (!item.requires) return true
        const { resource, action } = item.requires
        if (resource === 'tenant_admin' && action === 'admin')
          return perms.canTenantAdmin
        if (resource === 'system' && action === 'read')
          return perms.canSystemRead
        // 매트릭스 외 — 안전 default false (보수적).
        return false
      }),
    }))
    .filter((g) => g.items.length > 0)
}
