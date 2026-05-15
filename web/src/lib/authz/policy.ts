// rosshield Web Console — client-side RBAC PDP mirror.
//
// 본 모듈은 server `internal/platform/authz` (decision.go + permission_matrix.go +
// policy.go) 의 client mirror 입니다. 서버 측 결정 테이블 (§3.3 design doc) 을
// TypeScript로 정확 복제하여 web UI button gate · sidebar 가시성 · router-guard
// redirect 결정에 사용합니다.
//
// 보안 경계:
//   - 실 보안 경계는 server `RequirePermission` middleware 가 강제 — 본 모듈은 UX
//     선차단 용도. client-side 평가가 ALLOW를 반환해도 server가 DENY 하면 403.
//   - client mirror가 server와 어긋나는 경우 server 결정이 최종 — 본 모듈은 단순한
//     UX 친화 layer.
//
// 일관성 보장:
//   - SystemRolePermissions 매트릭스는 server permission_matrix.go와 정확 일치.
//     server 매트릭스 변경 시 본 파일 동기화 필요 — 향후 generated 코드 후보 (Phase 6).
//   - decide() 평가 순서는 server decision.go::Decide와 동일.

// Resource — 권한 결정의 객체 차원 (§3.3 매트릭스 9개).
export type Resource =
  | 'robot'
  | 'scan'
  | 'report'
  | 'insight'
  | 'audit'
  | 'fleet'
  | 'compliance'
  | 'tenant_admin' // sso·webhook·users·invitation 통합
  | 'system' // backup·integrity

// Action — 권한 결정의 동작 차원 (§3.3 매트릭스 6개).
export type Action = 'read' | 'write' | 'execute' | 'admin' | 'verify' | 'export'

// ScopeType — 권한 binding 범위 (§3.2).
//   - 'tenant' : tenant 전체 (모든 fleet implicit 적용).
//   - 'fleet'  : 특정 fleet ID 한정 — Subject.fleetId 가 ScopeID와 일치해야 함.
export type ScopeType = 'tenant' | 'fleet'

// 시스템 role 이름 (§3.1 — 권장 시드 6개). server `policy.go::Role*` 일치.
export const ROLE_OWNER = 'owner'
export const ROLE_ADMIN = 'admin'
export const ROLE_FLEET_ADMIN = 'fleet-admin'
export const ROLE_OPERATOR = 'operator'
export const ROLE_AUDITOR = 'auditor'
export const ROLE_READ_ONLY = 'read-only'

// Permission — 단일 (resource, action) 결정 단위. '*' 와일드카드는 양쪽 axis 독립.
//   - {resource:'*', action:'*'} → 모든 (r, a)에 매치 (owner).
export interface Permission {
  resource: Resource | '*'
  action: Action | '*'
}

// RoleBinding — 한 사용자가 가진 role 한 건과 그 scope.
export interface RoleBinding {
  role: string // §3.1 시드 6개 또는 사용자 정의 (Phase 6+).
  scopeType: ScopeType
  scopeId?: string // scopeType='fleet' 일 때만 fleet ID, 'tenant'면 undefined.
}

// Subject — 권한 평가 입력. 호출자(요청자) 정보.
//   - bindings : 사용자가 보유한 role binding 슬라이스.
//   - fleetId  : 평가가 향한 fleet (없으면 undefined — tenant 글로벌 평가).
export interface Subject {
  bindings: RoleBinding[]
  fleetId?: string
}

// matchesPermission — 본 permission이 (resource, action) 요청을 충족하는지 반환.
//   와일드카드는 양쪽 axis 독립적으로 동작.
function matchesPermission(p: Permission, resource: Resource, action: Action): boolean {
  if (p.resource !== '*' && p.resource !== resource) return false
  if (p.action !== '*' && p.action !== action) return false
  return true
}

// SystemRolePermissions — 시스템 role 6개의 정적 permission 셋.
//
// server `internal/platform/authz/permission_matrix.go::SystemRolePermissions` 와
// 정확 일치. design doc §3.3 매트릭스를 그대로 옮긴 것. 매트릭스 cell이 "—" 인
// (resource, action)은 어떤 role의 permission 리스트에도 등장하지 않습니다 — 즉
// owner를 제외한 어느 role도 해당 칸 미통과.
export const SystemRolePermissions: Readonly<Record<string, ReadonlyArray<Permission>>> = {
  // owner — 모든 (resource, action) implicit 통과 (§3.1).
  [ROLE_OWNER]: [{ resource: '*', action: '*' }],

  // admin — tenant 글로벌 관리. §3.3 매트릭스 "adm" 등장 cell 모두.
  [ROLE_ADMIN]: [
    { resource: 'robot', action: 'read' },
    { resource: 'robot', action: 'write' },
    { resource: 'robot', action: 'admin' },
    { resource: 'robot', action: 'export' },
    { resource: 'scan', action: 'read' },
    { resource: 'scan', action: 'execute' },
    { resource: 'scan', action: 'admin' },
    { resource: 'scan', action: 'export' },
    { resource: 'report', action: 'read' },
    { resource: 'report', action: 'admin' },
    { resource: 'report', action: 'verify' },
    { resource: 'report', action: 'export' },
    { resource: 'insight', action: 'read' },
    { resource: 'insight', action: 'write' },
    { resource: 'insight', action: 'execute' },
    { resource: 'insight', action: 'admin' },
    { resource: 'audit', action: 'read' },
    { resource: 'audit', action: 'verify' },
    { resource: 'audit', action: 'export' },
    { resource: 'fleet', action: 'read' },
    { resource: 'fleet', action: 'write' },
    { resource: 'fleet', action: 'admin' },
    { resource: 'compliance', action: 'read' },
    { resource: 'compliance', action: 'write' },
    { resource: 'compliance', action: 'execute' },
    { resource: 'compliance', action: 'admin' },
    { resource: 'compliance', action: 'export' },
    { resource: 'tenant_admin', action: 'read' },
    { resource: 'tenant_admin', action: 'admin' },
    { resource: 'system', action: 'read' },
    { resource: 'system', action: 'admin' },
  ],

  // fleet-admin — 특정 fleet 한정 admin. §3.3 매트릭스 "fadm" 등장 cell.
  [ROLE_FLEET_ADMIN]: [
    { resource: 'robot', action: 'read' },
    { resource: 'robot', action: 'write' },
    { resource: 'robot', action: 'admin' },
    { resource: 'scan', action: 'read' },
    { resource: 'scan', action: 'execute' },
    { resource: 'scan', action: 'admin' },
    { resource: 'report', action: 'read' },
    { resource: 'report', action: 'admin' },
    { resource: 'insight', action: 'read' },
    { resource: 'insight', action: 'write' },
    { resource: 'insight', action: 'execute' },
    { resource: 'fleet', action: 'read' },
    { resource: 'fleet', action: 'write' },
    { resource: 'compliance', action: 'read' },
    { resource: 'compliance', action: 'execute' },
  ],

  // operator — fleet 한정 일상 운영. §3.3 매트릭스 "op" 등장 cell.
  [ROLE_OPERATOR]: [
    { resource: 'robot', action: 'read' },
    { resource: 'robot', action: 'write' },
    { resource: 'scan', action: 'read' },
    { resource: 'scan', action: 'execute' },
    { resource: 'report', action: 'read' },
    { resource: 'insight', action: 'read' },
    { resource: 'fleet', action: 'read' },
    { resource: 'compliance', action: 'read' },
  ],

  // auditor — tenant 글로벌 read-only + verify/export. §3.3 매트릭스 "aud" 등장 cell.
  [ROLE_AUDITOR]: [
    { resource: 'robot', action: 'read' },
    { resource: 'robot', action: 'export' },
    { resource: 'scan', action: 'read' },
    { resource: 'scan', action: 'export' },
    { resource: 'report', action: 'read' },
    { resource: 'report', action: 'verify' },
    { resource: 'report', action: 'export' },
    { resource: 'insight', action: 'read' },
    { resource: 'audit', action: 'read' },
    { resource: 'audit', action: 'verify' },
    { resource: 'audit', action: 'export' },
    { resource: 'fleet', action: 'read' },
    { resource: 'compliance', action: 'read' },
    { resource: 'compliance', action: 'export' },
    { resource: 'system', action: 'read' },
  ],

  // read-only — tenant 글로벌 read-only. §3.3 매트릭스 "ro" 등장 cell.
  [ROLE_READ_ONLY]: [
    { resource: 'robot', action: 'read' },
    { resource: 'scan', action: 'read' },
    { resource: 'report', action: 'read' },
    { resource: 'insight', action: 'read' },
    { resource: 'fleet', action: 'read' },
    { resource: 'compliance', action: 'read' },
  ],
}

// isTenantScopedRole — role이 tenant 글로벌(모든 fleet implicit)인지 반환 (§3.2).
//   owner/admin/auditor/read-only — tenant scope. fleet-admin/operator — fleet scope.
//   사용자 정의 role은 호출자가 binding scopeType으로 명시.
export function isTenantScopedRole(roleName: string): boolean {
  return (
    roleName === ROLE_OWNER ||
    roleName === ROLE_ADMIN ||
    roleName === ROLE_AUDITOR ||
    roleName === ROLE_READ_ONLY
  )
}

// decide — Subject가 (resource, action) 권한을 가지는지 평가.
//
// 평가 순서 (server `decision.go::Decide` 와 동일):
//   1. subject.bindings 빈 슬라이스 → DENY.
//   2. 각 binding 순회 — 다음 모두 만족하면 ALLOW:
//      a. binding.role 이 SystemRolePermissions에 있고 그 permission 셋이
//         (resource, action) 매치.
//      b. binding이 fleet scope면 subject.fleetId 가 binding.scopeId와 일치.
//         binding이 tenant scope면 fleet 매칭 무시 (모든 fleet implicit 통과).
//   3. 어떤 binding도 매치 안 하면 DENY.
//
// 본 함수는 mutation 0 — 입력 인자 동일성 보존 (불변성).
export function decide(
  subject: Subject,
  resource: Resource,
  action: Action,
): boolean {
  if (!subject.bindings || subject.bindings.length === 0) return false

  for (const b of subject.bindings) {
    const perms = SystemRolePermissions[b.role]
    if (!perms) continue // 알려지지 않은 role — 시스템 role만 평가 (서버와 동일).

    let matched = false
    for (const p of perms) {
      if (matchesPermission(p, resource, action)) {
        matched = true
        break
      }
    }
    if (!matched) continue

    // scope 검증 — fleet scope binding은 subject.fleetId와 scopeId 정확 일치.
    // tenant scope binding은 모든 fleet에 implicit 통과.
    if (b.scopeType === 'fleet') {
      if (!b.scopeId) continue // 잘못된 binding — skip.
      if (b.scopeId !== subject.fleetId) continue
    }

    return true
  }

  return false
}

// bindingsFromUser — User 정보(roles · 향후 bindings 필드)에서 Subject용 binding
// 슬라이스를 생성합니다. server middleware `bindingsForSubject` 와 동일한 D-RBAC-7
// 호환 정책:
//   - user.bindings 비어 있지 않음 → 그대로 사용 (Stage 3+ 응답).
//   - 비어 있음 → user.roles 를 모두 tenant scope binding으로 fallback (옛 응답).
//
// 향후 server `/me` 응답에 bindings 필드가 추가되면 자동 사용 — 현재는 roles fallback
// 이 유일한 정보원입니다.
export function bindingsFromUser(user: {
  roles?: ReadonlyArray<string> | null
  bindings?: ReadonlyArray<RoleBinding> | null
}): RoleBinding[] {
  if (user.bindings && user.bindings.length > 0) {
    return user.bindings.map((b) => ({
      role: b.role,
      scopeType: b.scopeType,
      scopeId: b.scopeId,
    }))
  }
  if (!user.roles || user.roles.length === 0) return []
  return user.roles.map((r) => ({ role: r, scopeType: 'tenant' as const }))
}
