// B5 — `/license` 페이지 helper 단위 테스트.
//
// 페이지 마운트 자체는 TanStack Router 의존이라 회피 — 다른 페이지(advisor, compliance,
// integrations) 와 동일한 패턴으로 helper export 함수만 검증.

import { describe, expect, it } from 'vitest'

import {
  KNOWN_LICENSE_FEATURES,
  editionBadgeVariant,
  editionLabelKey,
  featureLabelKey,
  formatExpiresIn,
  quotaDisplay,
} from './license'

describe('editionLabelKey', () => {
  it('community → settings.license.edition.community', () => {
    expect(editionLabelKey('community')).toBe(
      'settings.license.edition.community',
    )
  })
  it('enterprise → settings.license.edition.enterprise', () => {
    expect(editionLabelKey('enterprise')).toBe(
      'settings.license.edition.enterprise',
    )
  })
})

describe('editionBadgeVariant', () => {
  it('enterprise → default (강조)', () => {
    expect(editionBadgeVariant('enterprise')).toBe('default')
  })
  it('community → secondary', () => {
    expect(editionBadgeVariant('community')).toBe('secondary')
  })
})

describe('quotaDisplay', () => {
  it('0 → "Unlimited" sentinel', () => {
    expect(quotaDisplay(0)).toBe('unlimited')
  })
  it('양수 → 숫자 문자열', () => {
    expect(quotaDisplay(100)).toBe('100')
    expect(quotaDisplay(1)).toBe('1')
  })
  it('음수도 안전하게 처리(스펙상 미발생이지만 방어)', () => {
    expect(quotaDisplay(-1)).toBe('-1')
  })
})

describe('featureLabelKey', () => {
  it('알려진 feature는 license.page.feature.<f> 키 반환', () => {
    expect(featureLabelKey('sso')).toBe('license.page.feature.sso')
    expect(featureLabelKey('mt')).toBe('license.page.feature.mt')
    expect(featureLabelKey('webhook')).toBe('license.page.feature.webhook')
    expect(featureLabelKey('cloud')).toBe('license.page.feature.cloud')
    expect(featureLabelKey('ha')).toBe('license.page.feature.ha')
  })
})

describe('KNOWN_LICENSE_FEATURES', () => {
  it('SSO·MT·Webhook·Cloud·HA 5종을 포함', () => {
    expect(KNOWN_LICENSE_FEATURES).toEqual(['sso', 'mt', 'webhook', 'cloud', 'ha'])
  })
})

describe('formatExpiresIn', () => {
  // 기준 시각: 2026-05-08T00:00:00Z (사용자 currentDate 참고)
  const now = new Date('2026-05-08T00:00:00Z')

  it('미래 만료일(>=2일) → "in N days"', () => {
    const iso = '2026-05-15T00:00:00Z' // +7d
    const result = formatExpiresIn(iso, now)
    expect(result).toEqual({ kind: 'future', days: 7 })
  })
  it('미래 만료일(1일 이내, 시간 단위) → "future" + days 0', () => {
    const iso = '2026-05-08T05:00:00Z' // +5h
    const result = formatExpiresIn(iso, now)
    expect(result).toEqual({ kind: 'future', days: 0 })
  })
  it('이미 만료 → "past" + days 양수(경과일)', () => {
    const iso = '2026-05-01T00:00:00Z' // -7d
    const result = formatExpiresIn(iso, now)
    expect(result).toEqual({ kind: 'past', days: 7 })
  })
  it('빈 문자열·null·undefined → kind: "none"', () => {
    expect(formatExpiresIn(undefined, now).kind).toBe('none')
    expect(formatExpiresIn('', now).kind).toBe('none')
  })
  it('잘못된 ISO → kind: "none"(방어)', () => {
    expect(formatExpiresIn('not-a-date', now).kind).toBe('none')
  })
})
