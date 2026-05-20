// D-UI-1 Stage 5b additional — 잔여 페이지 4건 axe-core 자동 scan.
//
// 대상 페이지 (carryover C5b-4 인증 전 + C5b-5 admin role · form-heavy):
//   1. /login                          — 인증 진입점 (form: email + password)
//   2. /invitations/accept/{token}     — 비인증 invitation 수락 (form: email + name + password)
//   3. /settings                       — 사용자/테넌트/라이선스/about 카드
//   4. /users                          — 초대 생성 + 활성 초대 테이블 (admin only)
//   5. /system                         — Health/HA/License/Backups/Usage/Severity/Packs
//
// 각 페이지를 jsdom 위에서 mount 후 `axe(container)` 호출.
// API hooks · TanStack Router · sonner toast는 모듈 mock으로 stub —
// 본 test의 목적은 **a11y(role · aria · label · landmark)** 검증이지
// 데이터 흐름·라우팅·persistence가 아니다.
//
// jsdom 한계로 color-contrast rule은 disable (`src/test/axe.ts` 참고).
//
// 다크 모드는 별도 scan: `<html class="dark">`로 토큰 swap 후 동일 component mount.
//
// 본 파일은 기존 `a11y.test.tsx`(Overview/Findings/Scans/Robots/Fleets)와 동거하며,
// 누적 cover 9 페이지 (5 + 4 신규)로 admin/auditor + 인증 전 surface 거의 전체 도달.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { axe } from '@/test/axe'
import type { PropsWithChildren, ReactElement } from 'react'

// ────────────────────────────────────────────────────────────────────────
// 1) Module mocks — 페이지 hook · 라우터 · 외부 부수효과 stub.
// ────────────────────────────────────────────────────────────────────────

// useNavigate / useSearch / Link / createFileRoute stub.
vi.mock('@tanstack/react-router', async () => {
  const React = await import('react')
  return {
    createFileRoute: () => () => ({ component: () => null }),
    Link: ({ children, to: _to, ...rest }: PropsWithChildren<Record<string, unknown>>) =>
      React.createElement('a', { href: '#mock', ...rest }, children),
    useNavigate: () => vi.fn(),
    useSearch: () => ({}),
    useParams: () => ({ token: 'tok-mock-axe-scan' }),
    useRouter: () => ({ navigate: vi.fn() }),
  }
})

// sonner toast.
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

// imperative confirm dialog — 본 scan에서는 호출 안 됨.
vi.mock('@/lib/confirm', () => ({
  confirm: vi.fn().mockResolvedValue(true),
}))

// API hooks 통째 stub.
vi.mock('@/api/hooks', () => {
  const empty = <T,>(data: T) => ({
    data,
    isPending: false,
    isLoading: false,
    isFetching: false,
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

  // /me 응답 mock — Settings 페이지의 user 카드용. admin 권한 가정.
  const meData = {
    id: 'usr-test-admin',
    email: 'admin@example.com',
    displayName: 'Test Admin',
    tenantId: 'tnt-test',
    roles: ['admin'],
  }

  // /license 응답 mock — Settings + System LicenseCard 분기 cover (enterprise).
  const licenseData = {
    edition: 'enterprise' as const,
    expired: false,
    issuedTo: 'Acme Corp',
    issuedAt: '2026-01-01T00:00:00Z',
    expiresAt: '2027-01-01T00:00:00Z',
    features: ['ha', 'llm'],
    quotas: {
      robotsMax: 100,
      scansPerDay: 1000,
      llmTokensPerDay: 50000,
    },
  }

  // /invitations 응답 mock — Users 페이지 테이블에 1행 노출 → 모든 cell · button 표면 scan.
  const invitations = [
    {
      id: 'inv-test-pending',
      email: 'invitee@example.com',
      roleName: 'auditor',
      invitedBy: 'usr-test-admin',
      expiresAt: '2099-01-01T00:00:00Z',
      createdAt: '2026-01-01T00:00:00Z',
    },
  ]

  // InvitationPreview mock — AcceptInvitation 페이지의 '활성' 분기 cover (form 노출).
  const invitationPreview = {
    email: 'invitee@example.com',
    roleName: 'auditor',
    expiresAt: '2099-01-01T00:00:00Z',
    accepted: false,
  }

  // /backups 응답 — System BackupsCard에 1행 노출.
  const backups = [
    {
      filename: 'backup-2026-05-19.tar.gz',
      size: 1024 * 1024 * 8,
      generatedAt: '2026-05-19T00:00:00Z',
      sha256: 'a'.repeat(64),
      includesEvidence: true,
    },
  ]

  // /usage-stats 응답 — System UsageStatsCard.
  const usageStats = {
    scansStarted: 42,
    scansCompletedSum: 40,
    scansCompleted: { completed: 38, failed: 1, cancelled: 1 },
    scanFailedChecks: 17,
  }

  // /scans 응답 — System ScansSeverityCard (terminal session 1건).
  const scans = [
    {
      id: 'scn-1',
      status: 'completed',
      severityCriticalFailed: 1,
      severityHighFailed: 2,
      severityMediumFailed: 3,
      severityLowFailed: 0,
    },
  ]

  return {
    // Queries.
    useMe: () => empty(meData),
    useLicenseInfo: () => empty(licenseData),
    useBackups: () => empty(backups),
    useUsageStats: () => empty(usageStats),
    useScans: () => empty(scans),
    usePacks: () => empty<unknown[]>([]),
    useInvitations: () => empty(invitations),
    useInvitationByToken: () => empty(invitationPreview),

    // Mutations.
    useLogin: mutation,
    useAcceptInvitation: mutation,
    useCreateInvitation: mutation,
    useDeleteInvitation: mutation,

    // RBAC — admin true (모든 CTA 노출 → 더 넓은 a11y 표면).
    useHasPermission: () => true,
    useHasRole: () => true,
    useIsAdmin: () => true,
    useIsAuditor: () => false,
    useIsAdminOrAuditor: () => true,
  }
})

// PWA offline indicator.
vi.mock('@/lib/use-is-offline', () => ({
  useIsOffline: () => false,
  mutationGuardTitle: () => undefined,
}))

// undoable — render 시 호출 안 됨.
vi.mock('@/lib/undoable', () => ({
  undoableAction: vi.fn(),
}))

// route-guards — users/system 페이지 createFileRoute beforeLoad 안에서 참조하지만,
//   createFileRoute 자체가 mock이라 호출되지 않는다. 그래도 import path 해소 위해 stub.
vi.mock('@/lib/route-guards', () => ({
  requirePermission: vi.fn(),
}))

// API client — system 페이지의 useHealthz가 직접 fetch + ApiError 사용.
//   본 scan에서는 fetch를 jsdom mock으로 대체.
vi.mock('@/api/client', () => ({
  API_BASE_PATH: '/api/v1',
}))

// ────────────────────────────────────────────────────────────────────────
// 2) 페이지 import — mock 이후에 import (호이스팅 보장).
// ────────────────────────────────────────────────────────────────────────

import { LoginPage } from '@/routes/login'
import { AcceptInvitationView } from '@/routes/invitations.accept.$token'
import { SettingsPage } from '@/routes/_authenticated/settings'
import { UsersPage } from '@/routes/_authenticated/users'
import { SystemPage } from '@/routes/_authenticated/system'

// ────────────────────────────────────────────────────────────────────────
// 3) Render helper — QueryClientProvider + main landmark.
// ────────────────────────────────────────────────────────────────────────

function renderPage(node: ReactElement): HTMLElement {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  const Wrapped = (): ReactElement => (
    <QueryClientProvider client={qc}>
      <main>{node}</main>
    </QueryClientProvider>
  )
  return render(<Wrapped />).container
}

// ────────────────────────────────────────────────────────────────────────
// 4) System 페이지 — useHealthz는 useQuery + 직접 fetch.
//    jsdom에 fetch가 없으므로 global.fetch 를 jest stub으로 대체.
// ────────────────────────────────────────────────────────────────────────

const healthzResponse = {
  status: 'ok',
  components: {
    storage: 'ok',
    eventbus: 'ok',
    scheduler: 'ok',
    signer: 'ok',
  },
  audit: { headSeq: 42, lastCheckpointSeq: 40, status: 'ok' },
  ha: {
    enabled: true,
    role: 'leader' as const,
    epoch: 3,
    leaderId: 'node-1',
    lastHeartbeatAt: '2026-05-19T00:00:00Z',
  },
}

beforeEach(() => {
  // window.fetch stub — useHealthz의 fetch('/healthz') 만 가로채면 충분.
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      statusText: 'OK',
      json: async () => healthzResponse,
    }),
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
})

// ────────────────────────────────────────────────────────────────────────
// 5) Light mode axe scan — 5 페이지 (Login·Invitation·Settings·Users·System).
// ────────────────────────────────────────────────────────────────────────

describe('D-UI-1 Stage 5b additional — a11y axe scan (light mode)', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
    document.documentElement.lang = 'ko'
  })

  it('Login 페이지: violation 0', async () => {
    const container = renderPage(<LoginPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })

  it('Invitation accept (active form) 페이지: violation 0', async () => {
    const container = renderPage(<AcceptInvitationView token="tok-mock" />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })

  it('Settings 페이지: violation 0', async () => {
    const container = renderPage(<SettingsPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })

  it('Users 페이지: violation 0', async () => {
    const container = renderPage(<UsersPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })

  it('System 페이지: violation 0', async () => {
    const container = renderPage(<SystemPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })
})

// ────────────────────────────────────────────────────────────────────────
// 6) Dark mode axe scan — 인증 전 (Login) + admin (Settings) 샘플.
//    나머지 페이지도 같은 token 사용 — 셋 cover 가 효율적.
// ────────────────────────────────────────────────────────────────────────

describe('D-UI-1 Stage 5b additional — a11y axe scan (dark mode)', () => {
  beforeEach(() => {
    document.documentElement.classList.add('dark')
    document.documentElement.lang = 'ko'
  })
  afterEach(() => {
    document.documentElement.classList.remove('dark')
  })

  it('Login 페이지(dark): violation 0', async () => {
    const container = renderPage(<LoginPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })

  it('Settings 페이지(dark): violation 0', async () => {
    const container = renderPage(<SettingsPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })

  it('Users 페이지(dark): violation 0', async () => {
    const container = renderPage(<UsersPage />)
    const results = await axe(container)
    expect(results).toHaveNoViolations()
  })
})
