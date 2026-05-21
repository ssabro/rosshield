// Phase 10.A-2/3/4 — `/regions` 페이지 단위 테스트.
//
// useReplicas / useAuditChainHeadSHA / useFailoverHistory hook을 mock해서
// 페이지가 5+ state를 분기 render 하는지 검증:
//   1. pending (skeleton)
//   2. success + primary + standby ×2 + audit head + cutover events
//   3. empty (replicas 0건)
//   4. error (ApiError + non-ApiError)
//   5. success + lag -1 unknown bucket
//
// 페이지는 createFileRoute에 emit되지만 본 test는 export된 RegionsPage component를
// 직접 mount — TanStack Router context는 불필요.

import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/api/errors'
import { useLocaleStore } from '@/i18n/store'

import type {
  AuditChainHeadSHA,
  FailoverHistoryResponse,
  ReplicasResponse,
} from '@/api/hooks'

interface ReplicasStub {
  data: ReplicasResponse | undefined
  isPending: boolean
  isError: boolean
  isSuccess: boolean
  error: unknown
}

interface AuditStub {
  data: AuditChainHeadSHA | undefined
  isPending: boolean
  isError: boolean
  isSuccess: boolean
  error: unknown
}

interface TimelineStub {
  data: FailoverHistoryResponse | undefined
  isPending: boolean
  isError: boolean
  isSuccess: boolean
  error: unknown
  dataUpdatedAt: number | undefined
}

const PENDING_REPLICAS: ReplicasStub = {
  data: undefined,
  isPending: true,
  isError: false,
  isSuccess: false,
  error: null,
}

const PENDING_AUDIT: AuditStub = {
  data: undefined,
  isPending: true,
  isError: false,
  isSuccess: false,
  error: null,
}

const PENDING_TIMELINE: TimelineStub = {
  data: undefined,
  isPending: true,
  isError: false,
  isSuccess: false,
  error: null,
  dataUpdatedAt: undefined,
}

const SUCCESS_AUDIT: AuditStub = {
  data: {
    tenantId: 'tn_demo',
    seq: 12,
    hashHex:
      'a1b2c3d4e5f60718a1b2c3d4e5f60718a1b2c3d4e5f60718a1b2c3d4e5f60718',
    updatedAt: '2026-05-21T11:59:55Z',
  },
  isPending: false,
  isError: false,
  isSuccess: true,
  error: null,
}

const SUCCESS_TIMELINE: TimelineStub = {
  data: {
    failovers: [
      {
        id: 2,
        fromRegion: 'us-east-1',
        toRegion: 'eu-west-1',
        initiatedByUser: 'us_admin_b',
        initiatedAt: '2026-05-21T11:00:00Z',
        completedAt: '2026-05-21T11:00:02Z',
        reason: 'planned cutover',
        status: 'completed',
      },
      {
        id: 1,
        fromRegion: 'ap-northeast-2',
        toRegion: 'us-east-1',
        initiatedByUser: 'us_admin_a',
        initiatedAt: '2026-05-21T10:00:00Z',
        reason: 'primary outage',
        status: 'in-progress',
      },
    ],
  },
  isPending: false,
  isError: false,
  isSuccess: true,
  error: null,
  dataUpdatedAt: Date.parse('2026-05-21T12:00:00Z'),
}

const stubHolder = vi.hoisted(() => ({
  replicas: {
    data: undefined,
    isPending: true,
    isError: false,
    isSuccess: false,
    error: null,
  } as ReplicasStub,
  audit: {
    data: undefined,
    isPending: true,
    isError: false,
    isSuccess: false,
    error: null,
  } as AuditStub,
  timeline: {
    data: undefined,
    isPending: true,
    isError: false,
    isSuccess: false,
    error: null,
    dataUpdatedAt: undefined,
  } as TimelineStub,
}))

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => () => ({ component: () => null }),
}))

vi.mock('@/api/hooks', () => ({
  useReplicas: () => stubHolder.replicas,
  useAuditChainHeadSHA: () => stubHolder.audit,
  useFailoverHistory: () => stubHolder.timeline,
}))

// mock이 호이스팅된 후 import — vi.mock은 ESM에서도 호이스팅되므로 일반 import 안전.
// eslint-disable-next-line import/first
import { RegionsPage } from './regions'

function setReplicas(next: ReplicasStub): void {
  stubHolder.replicas = next
}
function setAudit(next: AuditStub): void {
  stubHolder.audit = next
}
function setTimeline(next: TimelineStub): void {
  stubHolder.timeline = next
}

beforeEach(() => {
  // jsdom navigator.language는 'en-US'로 시작 → store 기본 'en'. 한국어 dict 검증을
  // 안정화하기 위해 매 test 시작 시 ko로 강제 (page 자체는 locale 무관 동작).
  useLocaleStore.getState().setLocale('ko')
})

afterEach(() => {
  setReplicas(PENDING_REPLICAS)
  setAudit(PENDING_AUDIT)
  setTimeline(PENDING_TIMELINE)
})

describe('RegionsPage', () => {
  it('pending → skeleton 표시 (불러오는 중)', () => {
    setReplicas(PENDING_REPLICAS)
    setAudit(PENDING_AUDIT)
    setTimeline(PENDING_TIMELINE)
    render(<RegionsPage />)
    // 'role=status'가 3개 — replicas skeleton + audit skeleton + timeline skeleton.
    expect(screen.getAllByRole('status').length).toBeGreaterThanOrEqual(1)
  })

  it('success + replicas 다건 → RegionHealthCard N개 render + lag 분기 노출', () => {
    setReplicas({
      data: {
        selfRegion: 'ap-northeast-2',
        selfRole: 'primary',
        replicas: [
          {
            region: 'ap-northeast-2',
            role: 'primary',
            endpoint: 'pg-primary.ap-northeast-2.internal:5432',
            lagSeconds: 3,
            lastReplayAt: '2026-05-21T11:59:55Z',
            lastHeartbeatAt: '2026-05-21T11:59:57Z',
            enabled: true,
          },
          {
            region: 'us-east-1',
            role: 'standby',
            endpoint: 'pg-standby.us-east-1.internal:5432',
            lagSeconds: 15,
            lastReplayAt: '2026-05-21T11:59:45Z',
            lastHeartbeatAt: '2026-05-21T11:59:47Z',
            enabled: true,
          },
          {
            region: 'eu-west-1',
            role: 'standby',
            endpoint: 'pg-standby.eu-west-1.internal:5432',
            lagSeconds: 60,
            lastReplayAt: '2026-05-21T11:59:00Z',
            lastHeartbeatAt: '2026-05-21T11:59:02Z',
            enabled: true,
          },
        ],
      },
      isPending: false,
      isError: false,
      isSuccess: true,
      error: null,
    })
    setAudit(SUCCESS_AUDIT)
    setTimeline(SUCCESS_TIMELINE)
    const { container } = render(<RegionsPage />)
    // region 이름은 RegionHealthCard + TimelineCard 양쪽에 등장 — getAllByText.
    expect(screen.getAllByText('ap-northeast-2').length).toBeGreaterThan(0)
    expect(screen.getAllByText('us-east-1').length).toBeGreaterThan(0)
    expect(screen.getAllByText('eu-west-1').length).toBeGreaterThan(0)
    expect(
      container.querySelector('[data-lag-bucket="healthy"]'),
    ).not.toBeNull()
    expect(
      container.querySelector('[data-lag-bucket="warning"]'),
    ).not.toBeNull()
    expect(
      container.querySelector('[data-lag-bucket="delayed"]'),
    ).not.toBeNull()
    // 신규 cards 모두 render.
    expect(container.querySelector('[data-card="audit-consistency"]')).not.toBeNull()
    expect(container.querySelector('[data-card="region-timeline"]')).not.toBeNull()
    // audit 일관 배지 = consistent (단일 self head).
    expect(
      container.querySelector('[data-consistency-status="consistent"]'),
    ).not.toBeNull()
    // timeline events 2개.
    expect(container.querySelectorAll('[data-failover-id]').length).toBe(2)
  })

  it('success + 0건 replicas → EmptyState 표시 + 2 cards 여전히 render', () => {
    setReplicas({
      data: {
        selfRegion: 'ap-northeast-2',
        selfRole: 'primary',
        replicas: [],
      },
      isPending: false,
      isError: false,
      isSuccess: true,
      error: null,
    })
    setAudit(SUCCESS_AUDIT)
    setTimeline({
      data: { failovers: [] },
      isPending: false,
      isError: false,
      isSuccess: true,
      error: null,
      dataUpdatedAt: undefined,
    })
    const { container } = render(<RegionsPage />)
    expect(screen.getByText('등록된 region 없음')).toBeInTheDocument()
    // 2 cards 여전히 표시.
    expect(container.querySelector('[data-card="audit-consistency"]')).not.toBeNull()
    expect(container.querySelector('[data-card="region-timeline"]')).not.toBeNull()
    // timeline empty 메시지.
    expect(container.textContent).toContain('cutover 이력 없음')
  })

  it('error (ApiError) → ApiError message 노출', () => {
    setReplicas({
      data: undefined,
      isPending: false,
      isError: true,
      isSuccess: false,
      error: new ApiError(503, 'replication not configured'),
    })
    setAudit(SUCCESS_AUDIT)
    setTimeline(SUCCESS_TIMELINE)
    render(<RegionsPage />)
    expect(
      screen.getByText(/replication not configured/i),
    ).toBeInTheDocument()
  })

  it('error (non-ApiError) → fallback dict 메시지 노출', () => {
    setReplicas({
      data: undefined,
      isPending: false,
      isError: true,
      isSuccess: false,
      error: new Error('network'),
    })
    setAudit(SUCCESS_AUDIT)
    setTimeline(SUCCESS_TIMELINE)
    render(<RegionsPage />)
    // 한국어 fallback: '리전 목록 조회 실패' (title + description 모두 표시)
    expect(
      screen.getAllByText(/리전 목록 조회 실패/).length,
    ).toBeGreaterThan(0)
  })

  it('success + lag -1 → unknown bucket render', () => {
    setReplicas({
      data: {
        selfRegion: 'ap-northeast-2',
        selfRole: 'primary',
        replicas: [
          {
            region: 'pending-region',
            role: 'standby',
            endpoint: 'pg.pending:5432',
            lagSeconds: -1,
            enabled: true,
          },
        ],
      },
      isPending: false,
      isError: false,
      isSuccess: true,
      error: null,
    })
    setAudit(SUCCESS_AUDIT)
    setTimeline(SUCCESS_TIMELINE)
    const { container } = render(<RegionsPage />)
    expect(
      container.querySelector('[data-lag-bucket="unknown"]'),
    ).not.toBeNull()
  })

  it('audit error → audit 에러 카드 표시', () => {
    setReplicas({
      data: {
        selfRegion: 'ap-northeast-2',
        selfRole: 'primary',
        replicas: [],
      },
      isPending: false,
      isError: false,
      isSuccess: true,
      error: null,
    })
    setAudit({
      data: undefined,
      isPending: false,
      isError: true,
      isSuccess: false,
      error: new ApiError(500, 'audit fetch failed'),
    })
    setTimeline(SUCCESS_TIMELINE)
    render(<RegionsPage />)
    expect(screen.getAllByText(/정합 정보 조회 실패/).length).toBeGreaterThan(0)
  })

  it('timeline error → timeline 에러 카드 표시', () => {
    setReplicas({
      data: {
        selfRegion: 'ap-northeast-2',
        selfRole: 'primary',
        replicas: [],
      },
      isPending: false,
      isError: false,
      isSuccess: true,
      error: null,
    })
    setAudit(SUCCESS_AUDIT)
    setTimeline({
      data: undefined,
      isPending: false,
      isError: true,
      isSuccess: false,
      error: new Error('timeline boom'),
      dataUpdatedAt: undefined,
    })
    render(<RegionsPage />)
    expect(screen.getAllByText(/이력 조회 실패/).length).toBeGreaterThan(0)
  })
})
