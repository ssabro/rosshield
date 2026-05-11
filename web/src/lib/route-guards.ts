import { redirect } from '@tanstack/react-router'

import { useAuthStore } from '@/stores/auth'

// RBAC Stage 2-E (Phase 5) — 페이지 router-level guard.
//
// Stage 2-D는 사이드바 메뉴 진입 자체를 숨겼지만, 비-admin 사용자가 직접 URL을
// 입력하면 페이지 진입은 가능했다. 본 guard는 createFileRoute beforeLoad에서
// 호출되어, 권한 미달 시 /overview로 redirect한다.
//
// 보안 경계는 server에서 RBAC Stage 1+2-A admin/auditor gate로 이미 강제 —
// 본 guard는 UX 정리(403 페이지를 보지 않게).
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

export function requireRole(...allowed: string[]): void {
  if (!hasAnyRole(allowed)) {
    throw redirect({ to: '/overview' })
  }
}
