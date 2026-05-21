// Phase 10.A-2 — `/regions` 페이지 단위 테스트.
//
// useReplicas hook을 mock해서 5가지 state를 cover:
//   1. pending (skeleton)
//   2. success + primary + standby ×2 (lag 분기 healthy / warning / delayed 포함)
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

import type { ReplicasResponse } from '@/api/hooks'

interface QueryStub {
  data: ReplicasResponse | undefined
  isPending: boolean
  isError: boolean
  isSuccess: boolean
  error: unknown
}

const PENDING_STUB: QueryStub = {
  data: undefined,
  isPending: true,
  isError: false,
  isSuccess: false,
  error: null,
}

const stubHolder = vi.hoisted(() => ({
  current: {
    data: undefined,
    isPending: true,
    isError: false,
    isSuccess: false,
    error: null,
  } as {
    data: ReplicasResponse | undefined
    isPending: boolean
    isError: boolean
    isSuccess: boolean
    error: unknown
  },
}))

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => () => ({ component: () => null }),
}))

vi.mock('@/api/hooks', () => ({
  useReplicas: () => stubHolder.current,
}))

// mock이 호이스팅된 후 import — vi.mock은 ESM에서도 호이스팅되므로 일반 import 안전.
// eslint-disable-next-line import/first
import { RegionsPage } from './regions'

function setStub(next: QueryStub): void {
  stubHolder.current = next
}

beforeEach(() => {
  // jsdom navigator.language는 'en-US'로 시작 → store 기본 'en'. 한국어 dict 검증을
  // 안정화하기 위해 매 test 시작 시 ko로 강제 (page 자체는 locale 무관 동작).
  useLocaleStore.getState().setLocale('ko')
})

afterEach(() => {
  setStub(PENDING_STUB)
})

describe('RegionsPage', () => {
  it('pending → skeleton 표시 (불러오는 중)', () => {
    setStub(PENDING_STUB)
    render(<RegionsPage />)
    expect(screen.getByRole('status')).toBeInTheDocument()
  })

  it('success + replicas 다건 → RegionHealthCard N개 render + lag 분기 노출', () => {
    setStub({
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
    const { container } = render(<RegionsPage />)
    expect(screen.getByText('ap-northeast-2')).toBeInTheDocument()
    expect(screen.getByText('us-east-1')).toBeInTheDocument()
    expect(screen.getByText('eu-west-1')).toBeInTheDocument()
    expect(
      container.querySelector('[data-lag-bucket="healthy"]'),
    ).not.toBeNull()
    expect(
      container.querySelector('[data-lag-bucket="warning"]'),
    ).not.toBeNull()
    expect(
      container.querySelector('[data-lag-bucket="delayed"]'),
    ).not.toBeNull()
  })

  it('success + 0건 replicas → EmptyState 표시', () => {
    setStub({
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
    render(<RegionsPage />)
    expect(screen.getByText('등록된 region 없음')).toBeInTheDocument()
  })

  it('error (ApiError) → ApiError message 노출', () => {
    setStub({
      data: undefined,
      isPending: false,
      isError: true,
      isSuccess: false,
      error: new ApiError(503, 'replication not configured'),
    })
    render(<RegionsPage />)
    expect(
      screen.getByText(/replication not configured/i),
    ).toBeInTheDocument()
  })

  it('error (non-ApiError) → fallback dict 메시지 노출', () => {
    setStub({
      data: undefined,
      isPending: false,
      isError: true,
      isSuccess: false,
      error: new Error('network'),
    })
    render(<RegionsPage />)
    // 한국어 fallback: '리전 목록 조회 실패' (title + description 모두 표시)
    expect(
      screen.getAllByText(/리전 목록 조회 실패/).length,
    ).toBeGreaterThan(0)
  })

  it('success + lag -1 → unknown bucket render', () => {
    setStub({
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
    const { container } = render(<RegionsPage />)
    expect(
      container.querySelector('[data-lag-bucket="unknown"]'),
    ).not.toBeNull()
  })
})
