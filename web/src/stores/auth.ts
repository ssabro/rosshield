import { create } from 'zustand'
import { persist, createJSONStorage } from 'zustand/middleware'

// rosshield Web Console 인증 스토어.
//
// C6 — refresh token은 서버가 HttpOnly cookie로 관리 (XSS 노출 차단).
// access token만 메모리/localStorage에 유지. 401 시 client middleware가 자동 refresh.
// 호환 메모: 이전 버전(C5 이전)에서 localStorage에 저장됐던 refreshToken 키는 hydration
// 시점에 무시되고 다음 setSession에서 사라진다.

export interface User {
  id: string
  email: string
  displayName: string
  tenantId: string
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
