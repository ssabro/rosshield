import { Outlet, createFileRoute, redirect } from '@tanstack/react-router'

import { Header } from '@/components/layout/Header'
import { Sidebar } from '@/components/layout/Sidebar'
import { OfflineIndicator } from '@/components/OfflineIndicator'
import { UpdatePrompt } from '@/components/UpdatePrompt'
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
      {/* PWA Stage 3 — top fixed banner / 우하단 toast (오프라인 + 갱신 알림). */}
      <OfflineIndicator />
      <UpdatePrompt />
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header />
        {/* D-UI-1 Stage 3 — Skip-to-content target. tabIndex={-1}로 anchor 점프 시
            screen-reader가 main으로 포커스 이동 가능. */}
        <main
          id="main-content"
          tabIndex={-1}
          className="flex-1 overflow-auto p-4 outline-none focus:outline-none md:p-6"
        >
          <Outlet />
        </main>
      </div>
    </div>
  )
}
