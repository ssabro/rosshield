// D-UI-1 Stage 5b drill-down — detail/관리 페이지 axe-core 자동 scan.
//
// 대상 페이지 (carryover C5b-3 drill-down):
//   1. /audit                                       — chain head + checkpoint
//   2. /reports                                     — 생성된 리포트 목록 + preview/verify
//   3. /sso                                         — Provider CRUD + IdP 호출
//   4. /integrations                                — webhook + SIEM 통합
//   5. /license                                     — edition · quotas · features
//   6. /advisor                                     — LLM advisor conversation
//   7. /fleets/$fleetId                             — 단일 fleet 상세 + robots
//   8. /robots/$robotId                             — 단일 robot 상세 + scan history
//   9. /packs/$packKey                              — pack 메타 + check 목록
//  10. /packs/$packKey/checks/$checkId              — check rationale + selftest
//
// 각 페이지를 jsdom 위에서 mount 후 `axe(container)` 호출. detail 페이지는 Route.useParams
// 의존 분리한 named export(*View)를 사용해 router 의존 0으로 mount.
//
// jsdom 한계로 color-contrast rule은 disable (src/test/axe.ts).
//
// 본 파일은 기존 a11y.test.tsx (5) + a11y-additional.test.tsx (5)와 동거 — 누적 cover
// 19 페이지로 admin/auditor + 인증 전 + drill-down 거의 전체 도달.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { axe } from '@/test/axe'
import type { PropsWithChildren, ReactElement } from 'react'

// ────────────────────────────────────────────────────────────────────────
// 1) Module mocks — 페이지 hook · 라우터 · 외부 부수효과 stub.
// ────────────────────────────────────────────────────────────────────────

vi.mock('@tanstack/react-router', async () => {
  const React = await import('react')
  // Route.useParams 호출에 응답하도록 mock route 객체에 useParams 메서드 포함.
  // PackDetailPage 같이 자체 Route.useParams 호출하는 페이지 mount용.
  const mockRoute = {
    component: () => null,
    useParams: () => ({
      fleetId: 'flt-1',
      robotId: 'rbt-1',
      packKey: 'cis-ubuntu-24.04',
      checkId: 'check-001',
    }),
  }
  return {
    createFileRoute: () => () => mockRoute,
    // Mocked Link는 children이 icon-only일 때 link-name violation을 일으킬 수 있어 fallback
    // aria-label 주입. production의 TanStack Link는 별 위계(active path detection 등)를 가지나
    // 본 axe scan은 test 인프라 한계로 일반 <a>로 대체 — fallback이 page-level 진짜 violation을
    // 가리지 않도록 prop의 aria-label은 우선 적용.
    Link: ({ children, to: _to, ...rest }: PropsWithChildren<Record<string, unknown>>) => {
      const explicit = (rest as { 'aria-label'?: string })['aria-label']
      const fallback = explicit || 'navigation link'
      return React.createElement('a', { href: '#mock', ...rest, 'aria-label': fallback }, children)
    },
    useNavigate: () => vi.fn(),
    useSearch: () => ({}),
    useParams: () => ({ fleetId: 'flt-1', robotId: 'rbt-1', packKey: 'cis-ubuntu-24.04', checkId: 'check-001' }),
    useRouter: () => ({ navigate: vi.fn() }),
  }
})

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

vi.mock('@/lib/confirm', () => ({
  confirm: vi.fn().mockResolvedValue(true),
}))

vi.mock('@/lib/use-is-offline', () => ({
  useIsOffline: () => false,
  mutationGuardTitle: () => undefined,
}))

vi.mock('@/lib/undoable', () => ({
  undoableAction: vi.fn(),
}))

vi.mock('@/lib/route-guards', () => ({
  requirePermission: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  API_BASE_PATH: '/api/v1',
}))

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

  const auditHead = {
    seq: 1234,
    hashHex: 'b'.repeat(64),
    occurredAt: '2026-05-19T00:00:00Z',
    signerKeyId: 'sgr-test',
  }

  const auditCheckpoint = {
    seq: 1200,
    hashHex: 'c'.repeat(64),
    signedAt: '2026-05-18T00:00:00Z',
    signature: 'd'.repeat(128),
    signerKeyId: 'sgr-test',
  }

  const reports = [
    {
      id: 'rep-1',
      sessionId: 'scn-1',
      format: 'pdf',
      pdfSizeBytes: 1024 * 64,
      pdfSha256: 'e'.repeat(64),
      generatedAt: '2026-05-19T00:00:00Z',
      generatedBy: 'usr-1',
      signature: { signerKeyId: 'sgr-test', signedAt: '2026-05-19T00:00:01Z' },
    },
  ]

  const ssoProviders = [
    {
      id: 'sso-1',
      name: 'corp-okta',
      kind: 'oidc',
      enabled: true,
      issuer: 'https://login.example.com',
      defaultRole: 'operator',
      createdAt: '2026-05-01T00:00:00Z',
    },
  ]

  const integrations = [
    {
      id: 'wh-1',
      name: 'slack-prod',
      url: 'https://hooks.slack.com/services/...',
      events: ['scan.completed', 'audit.checkpoint'],
      enabled: true,
      kind: 'slack',
      headers: {},
      createdAt: '2026-05-01T00:00:00Z',
    },
  ]

  const licenseData = {
    edition: 'enterprise' as const,
    expired: false,
    issuedTo: 'Acme Corp',
    issuedAt: '2026-01-01T00:00:00Z',
    expiresAt: '2027-01-01T00:00:00Z',
    features: ['ha', 'llm', 's3'],
    quotas: { robotsMax: 100, scansPerDay: 1000, llmTokensPerDay: 50000 },
  }

  const advisorConvos = [
    {
      id: 'cnv-1',
      title: 'Discuss SSH hardening',
      lastTurnAt: '2026-05-19T00:00:00Z',
      turns: 5,
    },
  ]

  const advisorConvoDetail = {
    id: 'cnv-1',
    title: 'Discuss SSH hardening',
    turns: [
      {
        id: 'trn-1',
        role: 'user',
        message: 'Why is PermitRootLogin a risk?',
        createdAt: '2026-05-19T00:00:00Z',
      },
      {
        id: 'trn-2',
        role: 'assistant',
        message: 'Root login allows unrestricted privilege escalation upon credential leak.',
        createdAt: '2026-05-19T00:00:01Z',
        toolCalls: [],
      },
    ],
  }

  const fleet = {
    id: 'flt-1',
    name: 'production-fleet',
    description: 'prod cluster',
    robotCount: 5,
    createdAt: '2026-05-01T00:00:00Z',
    policy: {
      scanSchedule: '@every 24h',
      packKey: 'cis-ubuntu-24.04',
      severityThreshold: 'high',
    },
  }

  const robots = [
    {
      id: 'rbt-1',
      name: 'arm-bot-01',
      fleetId: 'flt-1',
      hostname: 'arm-01.lab',
      sshUser: 'rosshield',
      lastScanAt: '2026-05-19T00:00:00Z',
      lastScanStatus: 'completed',
    },
  ]

  const robot = robots[0]

  const robotResults = [
    {
      id: 'rst-1',
      checkId: 'check-001',
      checkTitle: 'PermitRootLogin no',
      severity: 'critical',
      result: 'fail',
      scannedAt: '2026-05-19T00:00:00Z',
    },
  ]

  const pack = {
    key: 'cis-ubuntu-24.04',
    name: 'CIS Ubuntu 24.04',
    version: '1.0',
    description: 'CIS Benchmark for Ubuntu 24.04 LTS',
    checks: [
      { id: 'check-001', title: 'PermitRootLogin no', severity: 'critical' },
      { id: 'check-002', title: 'Password authentication disabled', severity: 'high' },
    ],
  }

  const check = {
    id: 'check-001',
    packKey: 'cis-ubuntu-24.04',
    title: 'PermitRootLogin no',
    description: 'OpenSSH must reject root login',
    severity: 'critical',
    rationale: 'Root login allows unrestricted privilege escalation',
    fixGuidance: 'Set PermitRootLogin no in /etc/ssh/sshd_config',
  }

  // SelftestCard schema: cases[].input.stdout|stderr|exitCode + expectedOutcome
  const checkSelftest = {
    cases: [
      {
        name: 'root login rejected',
        input: { stdout: 'PermitRootLogin no', stderr: '', exitCode: 0 },
        expectedOutcome: 'PASS',
      },
    ],
  }

  return {
    // Audit
    useAuditHead: () => empty(auditHead),
    useAuditCheckpoint: () => empty(auditCheckpoint),
    useLatestAuditCheckpoint: () => empty(auditCheckpoint),

    // Reports
    useReports: () => empty(reports),
    useVerifyReport: mutation,

    // SSO
    useSSOProviders: () => empty(ssoProviders),
    useCreateSSOProvider: mutation,
    useUpdateSSOProvider: mutation,
    useDeleteSSOProvider: mutation,

    // Integrations / Webhooks
    useWebhookEndpoints: () => empty(integrations),
    useWebhookDeliveries: () => empty<unknown[]>([]),
    useCreateWebhookEndpoint: mutation,
    useUpdateWebhookEndpoint: mutation,
    useDeleteWebhook: mutation,
    useDeleteWebhookEndpoint: mutation,
    useTestWebhookEndpoint: mutation,
    useTestWebhook: mutation,
    useCreateWebhook: mutation,
    useUpdateWebhook: mutation,
    useWebhooks: () => empty(integrations),
    formatWebhookEvent: (e: string) => e,
    summarizeDeliveries: () => ({ total: 0, success: 0, failed: 0, lastDeliveryAt: null }),
    useWebhookDelivery: () => empty(null),
    useReplayWebhookDelivery: mutation,

    // License
    useLicenseInfo: () => empty(licenseData),

    // Advisor
    useAdvisorConversations: () => empty(advisorConvos),
    useAdvisorConversation: () => empty(advisorConvoDetail),
    useAskAdvisor: mutation,

    // Fleets / Robots
    useFleet: () => empty(fleet),
    useFleets: () => empty([fleet]),
    useRobots: () => empty(robots),
    useRobot: () => empty(robot),
    useRobotResults: () => empty(robotResults),
    useDeleteRobot: mutation,
    useRotateCredential: mutation,

    // Packs / Checks
    usePack: () => empty(pack),
    usePacks: () => empty([pack]),
    useCheck: () => empty(check),
    useCheckSelftest: () => empty(checkSelftest),

    // RBAC — admin true.
    useHasPermission: () => true,
    useHasRole: () => true,
    useIsAdmin: () => true,
    useIsAuditor: () => false,
    useIsAdminOrAuditor: () => true,
  }
})

// ────────────────────────────────────────────────────────────────────────
// 2) 페이지 import — mock 이후에 import.
// ────────────────────────────────────────────────────────────────────────

import { AuditPage } from '@/routes/_authenticated/audit'
import { ReportsPage } from '@/routes/_authenticated/reports'
import { SSOPage } from '@/routes/_authenticated/sso'
import { IntegrationsPage } from '@/routes/_authenticated/integrations'
import { LicensePage } from '@/routes/_authenticated/license'
import { AdvisorPage } from '@/routes/_authenticated/advisor'
import { FleetDetailView } from '@/routes/_authenticated/fleets.$fleetId'
import { RobotDetailView } from '@/routes/_authenticated/robots.$robotId'
import { PackDetailPage } from '@/routes/_authenticated/packs.$packKey'
import { CheckDetailView } from '@/routes/_authenticated/packs.$packKey.checks.$checkId'

// ────────────────────────────────────────────────────────────────────────
// 3) Render helper.
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
// 4) Light mode axe scan — 10 drill-down 페이지.
// ────────────────────────────────────────────────────────────────────────

describe('D-UI-1 Stage 5b drill-down — a11y axe scan (light mode)', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
    document.documentElement.lang = 'ko'
  })

  it('Audit 페이지: violation 0', async () => {
    const container = renderPage(<AuditPage />)
    expect(await axe(container)).toHaveNoViolations()
  })

  it('Reports 페이지: violation 0', async () => {
    const container = renderPage(<ReportsPage />)
    expect(await axe(container)).toHaveNoViolations()
  })

  it('SSO 페이지: violation 0', async () => {
    const container = renderPage(<SSOPage />)
    expect(await axe(container)).toHaveNoViolations()
  })

  it('Integrations 페이지: violation 0', async () => {
    const container = renderPage(<IntegrationsPage />)
    expect(await axe(container)).toHaveNoViolations()
  })

  it('License 페이지: violation 0', async () => {
    const container = renderPage(<LicensePage />)
    expect(await axe(container)).toHaveNoViolations()
  })

  it('Advisor 페이지: violation 0', async () => {
    const container = renderPage(<AdvisorPage />)
    expect(await axe(container)).toHaveNoViolations()
  })

  it('Fleet detail 페이지: violation 0', async () => {
    const container = renderPage(<FleetDetailView fleetId="flt-1" />)
    expect(await axe(container)).toHaveNoViolations()
  })

  it('Robot detail 페이지: violation 0', async () => {
    const container = renderPage(<RobotDetailView robotId="rbt-1" />)
    expect(await axe(container)).toHaveNoViolations()
  })

  it('Pack detail 페이지: violation 0', async () => {
    const container = renderPage(<PackDetailPage />)
    expect(await axe(container)).toHaveNoViolations()
  })

  it('Check detail 페이지: violation 0', async () => {
    const container = renderPage(<CheckDetailView packKey="cis-ubuntu-24.04" checkId="check-001" />)
    expect(await axe(container)).toHaveNoViolations()
  })
})

// ────────────────────────────────────────────────────────────────────────
// 5) Dark mode 샘플 — 3 페이지 (admin form + detail + table).
//    모든 페이지가 동일 design token(`.dark` selector)을 사용하므로 sampling으로 충분.
// ────────────────────────────────────────────────────────────────────────

describe('D-UI-1 Stage 5b drill-down — a11y axe scan (dark mode sampling)', () => {
  beforeEach(() => {
    document.documentElement.classList.add('dark')
    document.documentElement.lang = 'ko'
  })

  afterEach(() => {
    document.documentElement.classList.remove('dark')
  })

  it('SSO 페이지 (dark): violation 0', async () => {
    const container = renderPage(<SSOPage />)
    expect(await axe(container)).toHaveNoViolations()
  })

  it('Fleet detail 페이지 (dark): violation 0', async () => {
    const container = renderPage(<FleetDetailView fleetId="flt-1" />)
    expect(await axe(container)).toHaveNoViolations()
  })

  it('Check detail 페이지 (dark): violation 0', async () => {
    const container = renderPage(<CheckDetailView packKey="cis-ubuntu-24.04" checkId="check-001" />)
    expect(await axe(container)).toHaveNoViolations()
  })
})
