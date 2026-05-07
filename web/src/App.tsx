import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { useEffect, useState } from 'react'

import { routeTree } from './routeTree.gen'
import { applyTheme, useThemeStore } from './stores/theme'

// rosshield Web Console 진입점.
// - TanStack Router(file-based) + TanStack Query 결선만 담당.
// - API 클라이언트(Stage B)·페이지(Stage C)·테스트(Stage D)는 후속 stage 영역.

const router = createRouter({
  routeTree,
  defaultPreload: 'intent',
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

export default function App(): React.ReactElement {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 30_000,
            refetchOnWindowFocus: false,
          },
        },
      }),
  )

  // 테마 적용 — 저장된 mode를 .dark 클래스로 반영하고, system 모드일 때는
  // prefers-color-scheme 변화도 추적한다.
  const theme = useThemeStore((s) => s.theme)
  useEffect(() => {
    applyTheme(theme)
    if (theme !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (): void => applyTheme('system')
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [theme])

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}
