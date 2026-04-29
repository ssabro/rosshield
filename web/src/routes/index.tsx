import { createFileRoute, redirect } from '@tanstack/react-router'

import { useAuthStore } from '@/stores/auth'

// 루트(`/`)는 자체 화면이 없다.
// - 토큰 보유 시 → `/robots`
// - 미로그인 시 → `/login`
// Phase 2의 Overview 대시보드가 들어오면 본 라우트는 Overview 페이지로 교체된다.
export const Route = createFileRoute('/')({
  beforeLoad: () => {
    const token = useAuthStore.getState().accessToken
    if (token) {
      throw redirect({ to: '/robots' })
    }
    throw redirect({ to: '/login' })
  },
})
