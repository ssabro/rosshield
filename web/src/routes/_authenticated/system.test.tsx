// D-UI-1 Stage 4 — `/system` helper 단위 테스트.
//
// 페이지 마운트는 TanStack Router + react-query 의존이라 회피 — users.test와 동일
// 패턴으로 export helper만 직접 검증. healthStatusToBadgeKind / Label 두 매핑이
// 백엔드 enum과 일치하는지(ok·degraded는 §HealthStatusStatus, fail은 sub-component
// 안전 fallback)에 초점.

import { describe, expect, it } from 'vitest'

import { healthStatusLabelKey, healthStatusToBadgeKind } from './system'

describe('healthStatusToBadgeKind', () => {
  it('ok → success', () => {
    expect(healthStatusToBadgeKind('ok')).toBe('success')
  })
  it('degraded → paused (운영자 주의)', () => {
    expect(healthStatusToBadgeKind('degraded')).toBe('paused')
  })
  it('fail → failed', () => {
    expect(healthStatusToBadgeKind('fail')).toBe('failed')
  })
  it('down → failed', () => {
    expect(healthStatusToBadgeKind('down')).toBe('failed')
  })
  it('대소문자 무관', () => {
    expect(healthStatusToBadgeKind('OK')).toBe('success')
    expect(healthStatusToBadgeKind('Degraded')).toBe('paused')
  })
  it('처음 보는 값은 unknown 으로 안전 fallback', () => {
    expect(healthStatusToBadgeKind('weird')).toBe('unknown')
    expect(healthStatusToBadgeKind('')).toBe('unknown')
  })
})

describe('healthStatusLabelKey', () => {
  it('ok → dict 키 ok', () => {
    expect(healthStatusLabelKey('ok')).toBe('system.health.status.ok')
  })
  it('degraded → dict 키 degraded', () => {
    expect(healthStatusLabelKey('degraded')).toBe('system.health.status.degraded')
  })
  it('fail → dict 키 fail', () => {
    expect(healthStatusLabelKey('fail')).toBe('system.health.status.fail')
  })
  it('처음 보는 값은 unknown 키로 fallback', () => {
    expect(healthStatusLabelKey('weird')).toBe('system.health.status.unknown')
  })
})
