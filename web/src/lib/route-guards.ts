import { redirect } from '@tanstack/react-router'

import { bindingsFromUser, decide } from '@/lib/authz/policy'
import { useAuthStore } from '@/stores/auth'

import type { Action, Resource } from '@/lib/authz/policy'

// RBAC Stage 2-E (Phase 5) — 페이지 router-level guard.
//
// Stage 2-D는 사이드바 메뉴 진입 자체를 숨겼지만, 비-admin 사용자가 직접 URL을
// 입력하면 페이지 진입은 가능했다. 본 guard는 createFileRoute beforeLoad에서
// 호출되어, 권한 미달 시 /overview로 redirect한다.
//
// 보안 경계는 server에서 RBAC Stage 1+2-A admin/auditor gate (Stage 4부터는
// RequirePermission resource×action 매트릭스)로 이미 강제 — 본 guard는 UX 정리
// (403 페이지를 보지 않게).
//
// fail-closed 정책:
//   - user.roles 미정의(구버전 persisted store) → redirect
//   - user.roles 빈 셋 → redirect
//   - allowed 셋과 교집합 0 → redirect
// 새 로그인 시 /me 응답에 roles 포함되어 자동 회복.

function hasAnyRole(allowed: readonly string[]): boolean {
  const roles = useAuthStore.getState().user?.roles
  if (!roles || roles.length === 0 || allowed.length === 0) return false
  for (const r of roles) {
    if (allowed.includes(r)) return true
  }
  return false
}

// requireRole — RBAC Stage 2-E 호환 보존 guard.
//
// @deprecated RBAC Stage 5 — role 이름 직접 매칭. 신규 라우트는
//   `requirePermission(resource, action, fleetId?)` 사용 권장.
export function requireRole(...allowed: string[]): void {
  if (!hasAnyRole(allowed)) {
    throw redirect({ to: '/overview' })
  }
}

// requirePermission — RBAC Stage 5 client-side router guard factory (design doc §7).
//
// 시그니처:
//   requirePermission(resource, action, fleetId?) → 함수 호출 시 권한 부족이면 throw.
//
// 동작:
//   1. useAuthStore에서 user(roles + 향후 bindings) 추출.
//   2. lib/authz/policy::bindingsFromUser 로 RoleBinding[] 빌드 (D-RBAC-7 fallback).
//   3. lib/authz/policy::decide 로 server `authz.Decide` 와 동일한 결정 평가.
//   4. ALLOW → return; DENY → redirect /overview.
//
// 보안 경계는 server `RequirePermission` middleware — 본 guard는 UX 선차단.
//
// 사용 예 (createFileRoute):
//   beforeLoad: () => requirePermission('tenant_admin', 'admin')
export function requirePermission(
  resource: Resource,
  action: Action,
  fleetId?: string,
): void {
  const user = useAuthStore.getState().user
  if (!user) {
    throw redirect({ to: '/overview' })
  }
  const subject = {
    bindings: bindingsFromUser({ roles: user.roles, bindings: user.bindings }),
    fleetId,
  }
  if (!decide(subject, resource, action)) {
    throw redirect({ to: '/overview' })
  }
}
