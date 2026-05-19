import { QueryClient } from '@tanstack/react-query'
import { PersistQueryClientProvider } from '@tanstack/react-query-persist-client'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { useEffect, useMemo, useState } from 'react'
import { Toaster } from 'sonner'

import { ConfirmDialogHost } from './components/common/ConfirmDialog'
import { SkipToContent } from './components/layout/SkipToContent'
import { useLocaleStore } from './i18n/store'
import {
  PERSIST_OPTIONS_BASE,
  createPersister,
} from './lib/persist/persister'
import { routeTree } from './routeTree.gen'
import { useAuthStore } from './stores/auth'
import { applyTheme, useThemeStore } from './stores/theme'

// rosshield Web Console 진입점.
// - TanStack Router(file-based) + TanStack Query 결선 + IndexedDB persist.
// - PWA persist Stage 2 (`pwa-persist-design.md` §7 Stage 2):
//   `QueryClientProvider` → `PersistQueryClientProvider` 교체. tenant 별
//   storage key + maxAge 7일 + 보안 차단 list(D-PWAPER-5) 결선.

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

  // PWA persist Stage 2 — tenant 별 IndexedDB storage key 분리(D-PWAPER-2).
  // tenant 변경 시 새 persister 인스턴스 생성 → key namespace 자동 갱신.
  // 미로그인(tenantId 없음) 상태는 `anon` namespace 사용.
  const tenantId = useAuthStore((s) => s.user?.tenantId)
  const persister = useMemo(() => createPersister({ tenantId }), [tenantId])

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

  // D-UI-1 Stage 3 — locale 변경 시 <html lang> 동적 갱신.
  // index.html 기본은 lang="ko"지만, 사용자가 EN 토글 시 screen-reader/검색엔진
  // 언어 추정이 정확해지도록 documentElement.lang 동기.
  const locale = useLocaleStore((s) => s.locale)
  useEffect(() => {
    if (typeof document === 'undefined') return
    document.documentElement.lang = locale
  }, [locale])

  return (
    <PersistQueryClientProvider
      client={queryClient}
      persistOptions={{ persister, ...PERSIST_OPTIONS_BASE }}
    >
      {/* D-UI-1 Stage 3 — KWCAG/WCAG 2.4.1: skip link는 모든 페이지 첫 focusable. */}
      <SkipToContent />
      <RouterProvider router={router} />
      {/* D-UI-1 Stage 2 — global toast + confirm host. */}
      <Toaster richColors closeButton position="top-right" />
      <ConfirmDialogHost />
    </PersistQueryClientProvider>
  )
}
