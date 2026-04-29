import { Outlet, createFileRoute, redirect } from '@tanstack/react-router'

import { Header } from '@/components/layout/Header'
import { Sidebar } from '@/components/layout/Sidebar'
import { useAuthStore } from '@/stores/auth'

// `_authenticated` pathless layout — 인증 가드 + Sidebar/Header 셸.
// - beforeLoad에서 token 확인 → 미보유 시 `/login`으로 redirect.
// - 자식 라우트(`/robots`, `/scans`, `/reports`)는 모두 이 셸 안에서 렌더링.
//
// 토큰 만료(401)는 `api/client.ts` middleware가 store를 클리어하지만,
// 실제 redirect는 다음 라우트 진입 시 이 가드가 처리한다.
export const Route = createFileRoute('/_authenticated')({
  beforeLoad: () => {
    const token = useAuthStore.getState().accessToken
    if (!token) {
      throw redirect({ to: '/login' })
    }
  },
  component: AuthenticatedLayout,
})

function AuthenticatedLayout(): React.ReactElement {
  return (
    <div className="flex min-h-screen bg-background text-foreground">
      <Sidebar />
      <div className="flex flex-1 flex-col">
        <Header />
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
