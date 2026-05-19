import { useMe } from '@/api/hooks'
import { Badge } from '@/components/ui/badge'
import { useT } from '@/i18n/t'
import { useAuthStore } from '@/stores/auth'

import type { DictKey } from '@/i18n/dict'

// D-UI-1 Stage 3 — Header에 현재 테넌트·역할을 가시화 (a11y review P0 보조).
//
// 표시 형식: "{tenantShort} · {role}"
//   - tenantShort: tenantId의 처음 8자 (서버가 tenant 이름을 따로 노출하지 않으므로
//     UUID prefix로 대신 — 추후 /me에 tenant.name 추가 시 본 컴포넌트만 수정).
//   - role: roles[0] 또는 'member' fallback. 본 표시는 현재 binding의 대표 역할
//     (admin > auditor > operator > viewer > member 순 우선).
//
// 사용자가 현재 어떤 테넌트·역할로 작업 중인지 즉시 알 수 있어 멀티테넌시 환경
// 실수 (다른 테넌트 데이터에 mutation) 가능성 감소.

const ROLE_PRIORITY = ['admin', 'auditor', 'operator', 'viewer', 'member'] as const

function pickPrimaryRole(roles: ReadonlyArray<string> | undefined): string {
  if (!roles || roles.length === 0) return 'member'
  for (const r of ROLE_PRIORITY) {
    if (roles.includes(r)) return r
  }
  return roles[0] ?? 'member'
}

function roleLabelKey(role: string): DictKey {
  if (role === 'admin') return 'header.role.admin'
  if (role === 'auditor') return 'header.role.auditor'
  if (role === 'operator') return 'header.role.operator'
  if (role === 'viewer') return 'header.role.viewer'
  return 'header.role.member'
}

export function TenantRoleBadge(): React.ReactElement | null {
  const t = useT()
  const me = useMe()
  const storeUser = useAuthStore((s) => s.user)
  const user = me.data ?? storeUser
  if (!user) return null

  const tenantShort = (user.tenantId ?? '').slice(0, 8) || '—'
  const primary = pickPrimaryRole(user.roles)
  const roleLabel = t(roleLabelKey(primary))

  return (
    <Badge
      variant="outline"
      className="hidden gap-1.5 px-2 py-1 text-[11px] font-normal sm:inline-flex"
      aria-label={t('header.tenantRole.aria')}
      title={`tenant: ${user.tenantId} · role: ${primary}`}
    >
      <span className="font-mono text-muted-foreground">{tenantShort}</span>
      <span aria-hidden className="text-muted-foreground/60">
        ·
      </span>
      <span className="font-medium">{roleLabel}</span>
    </Badge>
  )
}
