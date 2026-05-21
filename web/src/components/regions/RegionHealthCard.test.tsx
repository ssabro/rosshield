import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import {
  formatRelativeTime,
  lagBucket,
  lagLabelKey,
  lagTextClassName,
  RegionHealthCard,
  roleBadgeClassName,
  roleBadgeVariant,
  roleLabelKey,
} from './RegionHealthCard'

import type { RegionReplica } from '@/api/hooks'

// Phase 10.A-2 — RegionHealthCard helper + render 단위 테스트.
//
// helpers (lagBucket / roleBadgeVariant / formatRelativeTime 등)는 useT 의존 없이
// 분기 검증 — RegionHealthCard 본 컴포넌트는 useT를 사용하지만 i18n store가 SSR-safe
// default(ko)를 반환하므로 render 검증도 같은 파일에서 진행.

describe('lagBucket', () => {
  it('0 이하 -1 → unknown (last_replay_at zero)', () => {
    expect(lagBucket(-1)).toBe('unknown')
  })
  it('0 ≤ lag ≤ 5 → healthy', () => {
    expect(lagBucket(0)).toBe('healthy')
    expect(lagBucket(5)).toBe('healthy')
  })
  it('5 < lag ≤ 30 → warning', () => {
    expect(lagBucket(6)).toBe('warning')
    expect(lagBucket(15)).toBe('warning')
    expect(lagBucket(30)).toBe('warning')
  })
  it('lag > 30 → delayed', () => {
    expect(lagBucket(31)).toBe('delayed')
    expect(lagBucket(60)).toBe('delayed')
    expect(lagBucket(3600)).toBe('delayed')
  })
})

describe('lagLabelKey', () => {
  it('bucket별 dict 키 매핑', () => {
    expect(lagLabelKey('healthy')).toBe('regions.lag.healthy')
    expect(lagLabelKey('warning')).toBe('regions.lag.warning')
    expect(lagLabelKey('delayed')).toBe('regions.lag.delayed')
    expect(lagLabelKey('unknown')).toBe('regions.lag.unknown')
  })
})

describe('lagTextClassName', () => {
  it('healthy → emerald (정상)', () => {
    expect(lagTextClassName('healthy')).toContain('emerald')
  })
  it('warning → amber (주의)', () => {
    expect(lagTextClassName('warning')).toContain('amber')
  })
  it('delayed → red (지연)', () => {
    expect(lagTextClassName('delayed')).toContain('red')
  })
  it('unknown → muted', () => {
    expect(lagTextClassName('unknown')).toContain('muted')
  })
})

describe('roleBadgeVariant', () => {
  it('primary → default', () => {
    expect(roleBadgeVariant('primary')).toBe('default')
  })
  it('standby → secondary', () => {
    expect(roleBadgeVariant('standby')).toBe('secondary')
  })
  it('failed → destructive', () => {
    expect(roleBadgeVariant('failed')).toBe('destructive')
  })
  it('대소문자 무관', () => {
    expect(roleBadgeVariant('PRIMARY')).toBe('default')
    expect(roleBadgeVariant('Standby')).toBe('secondary')
  })
  it('처음 보는 값은 outline fallback', () => {
    expect(roleBadgeVariant('weird')).toBe('outline')
    expect(roleBadgeVariant('')).toBe('outline')
  })
})

describe('roleLabelKey', () => {
  it('각 role에 dict 키 매핑', () => {
    expect(roleLabelKey('primary')).toBe('regions.role.primary')
    expect(roleLabelKey('standby')).toBe('regions.role.standby')
    expect(roleLabelKey('failed')).toBe('regions.role.failed')
  })
  it('처음 보는 값은 unknown 키로 fallback', () => {
    expect(roleLabelKey('foo')).toBe('regions.role.unknown')
  })
})

describe('roleBadgeClassName', () => {
  it('primary → emerald 톤', () => {
    expect(roleBadgeClassName('primary')).toContain('emerald')
  })
  it('standby → sky 톤', () => {
    expect(roleBadgeClassName('standby')).toContain('sky')
  })
  it('failed → 빈 문자열 (destructive variant 기본 색 사용)', () => {
    expect(roleBadgeClassName('failed')).toBe('')
  })
})

describe('formatRelativeTime', () => {
  // 결정성을 위해 nowMs 인자 사용.
  const NOW = Date.parse('2026-05-21T12:00:00Z')

  it('빈 입력 → 빈 문자열', () => {
    expect(formatRelativeTime(undefined, NOW)).toBe('')
    expect(formatRelativeTime('', NOW)).toBe('')
  })
  it('invalid ISO → 빈 문자열', () => {
    expect(formatRelativeTime('not-a-date', NOW)).toBe('')
  })
  it('60초 미만 → "N초 전"', () => {
    expect(formatRelativeTime('2026-05-21T11:59:57Z', NOW)).toBe('3초 전')
  })
  it('60분 미만 → "N분 전"', () => {
    expect(formatRelativeTime('2026-05-21T11:55:00Z', NOW)).toBe('5분 전')
  })
  it('24시간 미만 → "N시간 전"', () => {
    expect(formatRelativeTime('2026-05-21T09:00:00Z', NOW)).toBe('3시간 전')
  })
  it('24시간 이상 → "N일 전"', () => {
    expect(formatRelativeTime('2026-05-19T12:00:00Z', NOW)).toBe('2일 전')
  })
})

// ── render ──

const baseReplica: RegionReplica = {
  region: 'ap-northeast-2',
  role: 'primary',
  endpoint: 'pg-primary.ap-northeast-2.internal:5432',
  lastReplayLsn: '0/16B4F00',
  lastReplayAt: '2026-05-21T11:59:55Z',
  lastHeartbeatAt: '2026-05-21T11:59:57Z',
  lagSeconds: 3,
  enabled: true,
}

describe('RegionHealthCard render', () => {
  it('primary replica에 region · endpoint · role · lag 표시', () => {
    const { container } = render(<RegionHealthCard replica={baseReplica} />)
    expect(screen.getByText('ap-northeast-2')).toBeInTheDocument()
    expect(
      screen.getByText('pg-primary.ap-northeast-2.internal:5432'),
    ).toBeInTheDocument()
    // data-role 속성으로 QA selector 안정성 확보.
    expect(container.querySelector('[data-role="primary"]')).not.toBeNull()
    // lag 3s는 healthy bucket — emerald 텍스트.
    expect(
      container.querySelector('[data-lag-bucket="healthy"]'),
    ).not.toBeNull()
  })

  it('lag 15s → warning(yellow)', () => {
    const { container } = render(
      <RegionHealthCard
        replica={{ ...baseReplica, role: 'standby', lagSeconds: 15 }}
      />,
    )
    expect(
      container.querySelector('[data-lag-bucket="warning"]'),
    ).not.toBeNull()
    expect(container.querySelector('[data-role="standby"]')).not.toBeNull()
  })

  it('lag 60s → delayed(red)', () => {
    const { container } = render(
      <RegionHealthCard
        replica={{ ...baseReplica, role: 'standby', lagSeconds: 60 }}
      />,
    )
    expect(
      container.querySelector('[data-lag-bucket="delayed"]'),
    ).not.toBeNull()
  })

  it('lag -1 → unknown(muted) + "—" 텍스트 표시', () => {
    const { container } = render(
      <RegionHealthCard
        replica={{
          ...baseReplica,
          lagSeconds: -1,
          lastReplayAt: undefined,
          lastHeartbeatAt: undefined,
        }}
      />,
    )
    expect(
      container.querySelector('[data-lag-bucket="unknown"]'),
    ).not.toBeNull()
    // lag 표시 영역에 "—" 들어감.
    expect(container.textContent).toContain('—')
  })

  it('failed role → destructive badge', () => {
    const { container } = render(
      <RegionHealthCard replica={{ ...baseReplica, role: 'failed' }} />,
    )
    expect(container.querySelector('[data-role="failed"]')).not.toBeNull()
  })
})
