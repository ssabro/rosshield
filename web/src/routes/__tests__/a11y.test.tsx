// D-UI-1 Stage 5 — 5 페이지 axe-core 자동 scan.
//
// 대상 페이지 (paying customer 자주 사용):
//   1. /overview (대시보드)
//   2. /findings (Insight 목록)
//   3. /scans    (스캔 세션 목록)
//   4. /robots   (로봇 목록)
//   5. /fleets   (Fleet 목록)
//
// 각 페이지를 jsdom 위에서 mount 후 `axe(container)` 호출.
// API hooks · TanStack Router · sonner toast는 모듈 mock으로 stub —
// 본 test의 목적은 **a11y(role · aria · label · landmark)** 검증이지
// 데이터 흐름·라우팅·persistence가 아니다.
//
// jsdom 한계로 color-contrast rule은 disable (`src/test/axe.ts` 참고).
//
// 다크 모드는 별도 scan: `<html class="dark">`로 토큰 swap 후 동일 component mount.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { axe } from '@/test/axe'
import type { PropsWithChildren, ReactElement } from 'react'

// ────────────────────────────────────────────────────────────────────────
// 1) Module mocks — 페이지 hook · 라우터 · 외부 부수효과 stub.
// ────────────────────────────────────────────────────────────────────────

// useNavigate / useSearch / Link / createFileRoute stub —
// jsdom mount 시 router context가 없어도 안전하게 동작하도록 한다.
vi.mock('@tanstack/react-router', async () => {
  const React = await import('react')
  return {
    createFileRoute: () => () => ({ component: () => null }),
    Link: ({ children, to: _to, ...rest }: PropsWithChildren<Record<string, unknown>>) =>
      React.createElement('a', { href: '#mock', ...rest }, children),
    useNavigate: () => vi.fn(),
    useSearch: () => ({}),
    useParams: () => ({}),
    useRouter: () => ({ navigate: vi.fn() }),
  }
})

// sonner toast는 portal mount + side-effect 가 본 test 의 범위를 벗어남.
vi.mock('@/lib/toast', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
    message: vi.fn(),
    promise: vi.fn(),
    dismiss: vi.fn(),
  },
}))

// confirm dialog는 imperative API라 mock으로 항상 true 반환.
vi.mock('@/lib/confirm', () => ({
  confirm: vi.fn().mockResolvedValue(true),
}))

// 모든 API hook을 통째 stub — 페이지 렌더에 필요한 query/mutation 결과만 제공.
vi.mock('@/api/hooks', () => {
  const empty = <T,>(data: T) => ({
    data,
    isPending: false,
    isLoading: false,
    isError: false,
    isSuccess: true,
    error: null,
    refetch: vi.fn(),
  })
  const mutation = () => ({
    mutate: vi.fn(),
    mutateAsync: vi.fn().mockResolvedValue(undefined),
    isPending: false,
    isError: false,
    isSuccess: false,
    error: null,
    reset: vi.fn(),
  })
  return {
    // Queries — 빈 배열/객체로 success state 노출 → EmptyState 분기를 axe scan.
    useRobots: () => empty<unknown[]>([]),
    useInsights: () => empty<unknown[]>([]),
    useScans: () => empty<unknown[]>([]),
    useFleets: () => empty<unknown[]>([]),
    useComplianceProfiles: () => empty<unknown[]>([]),
    useComplianceSnapshots: () => empty<unknown[]>([]),
    useAuditHead: () => empty<{ headHash: string; sequence: number } | null>(null),
    usePacks: () => empty<unknown[]>([]),
    useScan: () => empty<unknown>(null),
    useScanProgress: () => ({ status: 'idle', data: null }),
    useFleet: () => empty<unknown>(null),
    useRobot: () => empty<unknown>(null),
    useRobotResults: () => empty<unknown[]>([]),

    // Mutations.
    useDismissInsight: mutation,
    useCreateRobot: mutation,
    useCreateFleet: mutation,
    useUpdateFleet: mutation,
    useDeleteFleet: mutation,
    useStartScan: mutation,
    useCancelScan: mutation,
    useRotateCredential: mutation,
    useDeleteRobot: mutation,

    // Permissions / role — 기본은 admin true (모든 CTA 노출 → 더 넓은 axe scan 표면).
    useHasPermission: () => true,
    useHasRole: () => true,
    useIsAdmin: () => true,
    useIsAuditor: () => false,
    useIsAdminOrAuditor: () => true,

    // Utility.
    isTerminalScanStatus: (s: string) =>
      s === 'completed' || s === 'failed' || s === 'cancelled',
  }
})

// PWA offline indicator는 navigator.onLine 기반 — jsdom 기본 true.
vi.mock('@/lib/use-is-offline', () => ({
  useIsOffline: () => false,
  mutationGuardTitle: () => undefined,
}))

// undoable은 toast queue 의존 — render 시 호출 안 되므로 빈 stub.
vi.mock('@/lib/undoable', () => ({
  undoableAction: vi.fn(),
}))

// ────────────────────────────────────────────────────────────────────────
// 2) 페이지 import — mock 이후에 import (호이스팅 보장).
// ────────────────────────────────────────────────────────────────────────

import { OverviewPage } from '@/routes/_authenticated/overview'
import { FindingsPage } from '@/routes/_authenticated/findings'
import { ScansPage } from '@/routes/_authenticated/scans'
import { RobotsPage } from '@/routes/_authenticated/robots'
import { FleetsPage } from '@/routes/_authenticated/fleets'

// ────────────────────────────────────────────────────────────────────────
// 3) Render helper — QueryClientProvider 만 wrap (router는 mock).
// ────────────────────────────────────────────────────────────────────────

function renderPage(node: ReactElement): HTMLElement {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  // main landmark 보장 — 실제 App.tsx 에는 main 이 있으므로 axe region rule 만족.
  const Wrapped = (): ReactElement => (
    <QueryClientProvider client={qc}>
      <main>{node}</main>
    </QueryClientProvider>
  )
  return render(<Wrapped />).container
}

// ────────────────────────────────────────────────────────────────────────
// 4) Light mode axe scan — 5 페이지.
// ────────────────────────────────────────────────────────────────────────

describe('D-UI-1 Stage 5 — a11y axe scan (light mode)', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
    document.documentElement.lang = 'ko'
  })

  it('Overview 페이지: violation 0', async () => {
    const container = renderPage(<OverviewPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })

  it('Findings 페이지: violation 0', async () => {
    const container = renderPage(<FindingsPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })

  it('Scans 페이지: violation 0', async () => {
    const container = renderPage(<ScansPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })

  it('Robots 페이지: violation 0', async () => {
    const container = renderPage(<RobotsPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })

  it('Fleets 페이지: violation 0', async () => {
    const container = renderPage(<FleetsPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })
})

// ────────────────────────────────────────────────────────────────────────
// 5) Dark mode axe scan — Overview/Findings 만 sample (모두 동일 token).
// ────────────────────────────────────────────────────────────────────────

describe('D-UI-1 Stage 5 — a11y axe scan (dark mode)', () => {
  beforeEach(() => {
    document.documentElement.classList.add('dark')
    document.documentElement.lang = 'ko'
  })
  afterEach(() => {
    document.documentElement.classList.remove('dark')
  })

  it('Overview 페이지(dark): violation 0', async () => {
    const container = renderPage(<OverviewPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })

  it('Findings 페이지(dark): violation 0', async () => {
    const container = renderPage(<FindingsPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })
})
