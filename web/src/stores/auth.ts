import { create } from 'zustand'
import { persist } from 'zustand/middleware'

// rosshield Web Console 인증 스토어.
// - localStorage 영속(Phase 1 단순화 — R12-12). XSS 노출 트레이드오프는
//   Phase 2에서 HttpOnly cookie + CSRF 이중 방식으로 재검토.
// - 토큰 만료/401 회신 시 client.ts middleware가 clearSession을 호출한다.

export interface User {
  id: string
  email: string
  displayName: string
  tenantId: string
}

interface AuthState {
  accessToken: string | null
  refreshToken: string | null
  user: User | null
  setSession: (data: {
    accessToken: string
    refreshToken: string
    user: User
  }) => void
  clearSession: () => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      accessToken: null,
      refreshToken: null,
      user: null,
      setSession: ({ accessToken, refreshToken, user }) =>
        set({ accessToken, refreshToken, user }),
      clearSession: () =>
        set({ accessToken: null, refreshToken: null, user: null }),
    }),
    {
      name: 'rosshield-auth', // localStorage key
    },
  ),
)
