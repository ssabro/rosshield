import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { useState } from 'react'

import { routeTree } from './routeTree.gen'

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

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}
