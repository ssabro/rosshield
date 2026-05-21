import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import {
  RegionTimelineCard,
  statusBadgeClassName,
  statusBadgeVariant,
  statusDotClassName,
  statusLabelKey,
} from './RegionTimelineCard'

import { useLocaleStore } from '@/i18n/store'

import type { FailoverEvent } from '@/api/hooks'

// Phase 10.A-4 — RegionTimelineCard helper + render 단위 테스트.

beforeEach(() => {
  useLocaleStore.getState().setLocale('ko')
})

describe('statusBadgeVariant', () => {
  it('completed → default', () => {
    expect(statusBadgeVariant('completed')).toBe('default')
  })
  it('in-progress → secondary', () => {
    expect(statusBadgeVariant('in-progress')).toBe('secondary')
  })
  it('failed → destructive', () => {
    expect(statusBadgeVariant('failed')).toBe('destructive')
  })
})

describe('statusLabelKey', () => {
  it('각 status에 dict 키 매핑', () => {
    expect(statusLabelKey('completed')).toBe(
      'regions.timeline.status.completed',
    )
    expect(statusLabelKey('in-progress')).toBe(
      'regions.timeline.status.inProgress',
    )
    expect(statusLabelKey('failed')).toBe('regions.timeline.status.failed')
  })
})

describe('statusBadgeClassName', () => {
  it('completed → emerald 톤', () => {
    expect(statusBadgeClassName('completed')).toContain('emerald')
  })
  it('in-progress → amber 톤', () => {
    expect(statusBadgeClassName('in-progress')).toContain('amber')
  })
  it('failed → 빈 문자열 (destructive variant 기본 색 사용)', () => {
    expect(statusBadgeClassName('failed')).toBe('')
  })
})

describe('statusDotClassName', () => {
  it('각 status에 dot 색 매핑', () => {
    expect(statusDotClassName('completed')).toContain('emerald')
    expect(statusDotClassName('in-progress')).toContain('amber')
    expect(statusDotClassName('failed')).toContain('red')
  })
})

// ── fixture ──

const EVENTS: FailoverEvent[] = [
  {
    id: 3,
    fromRegion: 'eu-west-1',
    toRegion: 'ap-northeast-2',
    initiatedByUser: 'us_admin_c',
    initiatedAt: '2026-05-21T12:00:00Z',
    reason: 'third event',
    status: 'in-progress',
  },
  {
    id: 2,
    fromRegion: 'us-east-1',
    toRegion: 'eu-west-1',
    initiatedByUser: 'us_admin_b',
    initiatedAt: '2026-05-21T11:00:00Z',
    completedAt: '2026-05-21T11:00:02Z',
    auditEntryId: 42,
    reason: 'second event',
    status: 'completed',
  },
  {
    id: 1,
    fromRegion: 'ap-northeast-2',
    toRegion: 'us-east-1',
    initiatedByUser: 'us_admin_a',
    initiatedAt: '2026-05-21T10:00:00Z',
    completedAt: '2026-05-21T10:00:05Z',
    reason: 'first event',
    status: 'failed',
  },
]

describe('RegionTimelineCard render', () => {
  it('N개 event render — status 3종 분기 모두 표시', () => {
    const { container } = render(
      <RegionTimelineCard events={EVENTS} updatedAt="2026-05-21T12:00:00Z" />,
    )
    // 3 events × dot.
    expect(
      container.querySelectorAll('[data-failover-id]').length,
    ).toBe(3)
    // 각 status badge.
    expect(
      container.querySelector('[data-status="completed"]'),
    ).not.toBeNull()
    expect(
      container.querySelector('[data-status="in-progress"]'),
    ).not.toBeNull()
    expect(container.querySelector('[data-status="failed"]')).not.toBeNull()
    // from/to mono 표기 — fixture region이 여러 event에 반복 등장하므로 getAllByText.
    expect(screen.getAllByText('ap-northeast-2').length).toBeGreaterThan(0)
    expect(screen.getAllByText('us-east-1').length).toBeGreaterThan(0)
    expect(screen.getAllByText('eu-west-1').length).toBeGreaterThan(0)
  })

  it('각 event에 actor + reason 표시 — mock fixture 검증', () => {
    const { container } = render(<RegionTimelineCard events={EVENTS} />)
    // actor — 모든 fixture에 initiatedByUser 있음.
    expect(container.textContent).toContain('us_admin_a')
    expect(container.textContent).toContain('us_admin_b')
    expect(container.textContent).toContain('us_admin_c')
    // reason 텍스트.
    expect(container.textContent).toContain('first event')
    expect(container.textContent).toContain('second event')
    expect(container.textContent).toContain('third event')
  })

  it('empty (events 0건) → "cutover 이력 없음" 표시', () => {
    const { container } = render(<RegionTimelineCard events={[]} />)
    // ko dict.
    expect(container.textContent).toContain('cutover 이력 없음')
    // timeline list 자체는 미render.
    expect(container.querySelector('[data-timeline-list]')).toBeNull()
  })

  it('actor 미지정 event는 actor 라인 미표시', () => {
    const minimal: FailoverEvent[] = [
      {
        id: 99,
        fromRegion: 'a',
        toRegion: 'b',
        initiatedAt: '2026-05-21T12:00:00Z',
        status: 'completed',
      },
    ]
    const { container } = render(<RegionTimelineCard events={minimal} />)
    // 'Actor' 라벨이 ko dict에선 'Actor' 그대로 — 미표시 검증은 actor 텍스트 부재.
    expect(container.querySelector('[data-failover-id="99"]')).not.toBeNull()
    // initiatedByUser 없으면 ko dict 'Actor:' 줄 자체가 없음.
    expect(container.textContent).not.toContain('Actor:')
  })

  it('updatedAt props → 헤더에 갱신 시각 표시', () => {
    const { container } = render(
      <RegionTimelineCard events={EVENTS} updatedAt="2026-05-21T12:00:00Z" />,
    )
    // 갱신 시각 label (ko: '갱신 시각').
    expect(container.textContent).toContain('갱신 시각')
  })
})
