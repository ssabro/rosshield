import { Outlet, createRootRoute } from '@tanstack/react-router'

// 최상위 라우트는 Outlet만 노출한다.
// - 인증 영역은 `_authenticated/route.tsx`가 Sidebar/Header 레이아웃 제공.
// - `/login`은 자체 풀스크린 레이아웃.
// 이 분리로 로그인 페이지에 sidebar가 새지 않는다.
export const Route = createRootRoute({
  component: () => <Outlet />,
})
