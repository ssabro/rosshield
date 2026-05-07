import { createFileRoute, redirect } from '@tanstack/react-router'

import { useAuthStore } from '@/stores/auth'

// 루트(`/`)는 자체 화면이 없다.
// - 토큰 보유 시 → `/overview` (B1로 도입된 대시보드)
// - 미로그인 시 → `/login`
export const Route = createFileRoute('/')({
  beforeLoad: () => {
    const token = useAuthStore.getState().accessToken
    if (token) {
      throw redirect({ to: '/overview' })
    }
    throw redirect({ to: '/login' })
  },
})
