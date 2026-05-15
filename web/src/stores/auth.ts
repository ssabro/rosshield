import { create } from 'zustand'
import { persist, createJSONStorage } from 'zustand/middleware'

// rosshield Web Console 인증 스토어.
//
// C6 — refresh token은 서버가 HttpOnly cookie로 관리 (XSS 노출 차단).
// access token만 메모리/localStorage에 유지. 401 시 client middleware가 자동 refresh.
// 호환 메모: 이전 버전(C5 이전)에서 localStorage에 저장됐던 refreshToken 키는 hydration
// 시점에 무시되고 다음 setSession에서 사라진다.

// RBAC Stage 5 — RoleBindingDTO: server `tenant.RoleBindingClaim` 의 web mirror.
// 서버 `/me` 응답에 bindings 필드가 추가되면 자동 사용 (Stage 5 시점은 응답 미포함 →
// roles 슬라이스를 tenant scope로 fallback. lib/authz/policy.ts::bindingsFromUser 참조).
// 향후 server 응답 형식 변경 시 본 인터페이스만 호환 유지하면 web 측 자동 적용.
export interface RoleBindingDTO {
  role: string
  scopeType: 'tenant' | 'fleet'
  scopeId?: string
}

export interface User {
  id: string
  email: string
  displayName: string
  tenantId: string
  // RBAC Stage 2-B (Phase 5) — Login·/me 응답이 함께 내려주는 사용자 role 셋.
  // Web UI button conditional render(useIsAdmin·useHasRole)에 사용. 서버 측 admin/auditor
  // gate는 RBAC Stage 1+2-A에서 이미 강제 — 본 필드는 UX 선차단 용도.
  // 백엔드 호환을 위해 optional — 구버전 응답은 빈 셋으로 취급.
  roles?: string[]
  // RBAC Stage 5 (Phase 5) — fleet scope 포함 binding 셋 (D-RBAC-8).
  // 현 server `/me` 응답에는 미포함이지만 향후 추가 시 자동 사용. 부재 시 web은
  // roles 슬라이스를 tenant scope binding으로 fallback (D-RBAC-7 호환 정책).
  bindings?: RoleBindingDTO[]
}

interface AuthState {
  accessToken: string | null
  user: User | null
  setSession: (data: { accessToken: string; user: User }) => void
  setAccessToken: (token: string) => void
  clearSession: () => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      accessToken: null,
      user: null,
      setSession: ({ accessToken, user }) => set({ accessToken, user }),
      setAccessToken: (accessToken) => set({ accessToken }),
      clearSession: () => set({ accessToken: null, user: null }),
    }),
    {
      name: 'rosshield-auth', // localStorage key (C5 이전 형식과 동일 — 추가 마이그레이션 불필요)
      storage: createJSONStorage(() => localStorage),
      // accessToken·user만 영속. refreshToken 등 의도치 않은 필드는 hydration에서 drop.
      partialize: (s) => ({ accessToken: s.accessToken, user: s.user }),
    },
  ),
)
