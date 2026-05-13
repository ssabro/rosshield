// scans.test.tsx — SessionProgressCard pure helper 단위 테스트 (computeETA).
//
// route 파일은 createFileRoute 호출이라 RTL 직접 마운트 회피 — pure helper만 검증
// (findings.test.tsx 패턴).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { computeETA } from './scans'

describe('computeETA', () => {
  // 고정 now로 비결정성 제거. startedAt은 now 기준 elapsed 계산.
  const fixedNow = new Date('2026-05-13T10:00:00Z').getTime()

  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(fixedNow)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('running + 50% 진행 (10s elapsed, 5/10) → 약 10s 남음', () => {
    const startedAt = new Date(fixedNow - 10_000).toISOString() // 10초 전 시작
    expect(computeETA('running', startedAt, 5, 10)).toBe('10s')
  })

  it('running + 25% 진행 (60s elapsed, 1/4) → 3m 남음', () => {
    const startedAt = new Date(fixedNow - 60_000).toISOString()
    expect(computeETA('running', startedAt, 1, 4)).toBe('3m')
  })

  it('long-running (1h elapsed, 30/100) → 2h 20m 남음', () => {
    const startedAt = new Date(fixedNow - 3_600_000).toISOString()
    expect(computeETA('running', startedAt, 30, 100)).toBe('2h 20m')
  })

  it('completed status → null (ETA 무의미)', () => {
    const startedAt = new Date(fixedNow - 10_000).toISOString()
    expect(computeETA('completed', startedAt, 5, 10)).toBeNull()
  })

  it('failed/cancelled → null', () => {
    const startedAt = new Date(fixedNow - 10_000).toISOString()
    expect(computeETA('failed', startedAt, 5, 10)).toBeNull()
    expect(computeETA('cancelled', startedAt, 5, 10)).toBeNull()
  })

  it('startedAt 없으면 null (분모 부족)', () => {
    expect(computeETA('running', null, 5, 10)).toBeNull()
    expect(computeETA('running', undefined, 5, 10)).toBeNull()
    expect(computeETA('running', '', 5, 10)).toBeNull()
  })

  it('completed=0 → null (분모 0 회피, 첫 check 끝나야 추정 가능)', () => {
    const startedAt = new Date(fixedNow - 10_000).toISOString()
    expect(computeETA('running', startedAt, 0, 10)).toBeNull()
  })

  it('completed >= total → null (이미 완료, ETA 무의미)', () => {
    const startedAt = new Date(fixedNow - 10_000).toISOString()
    expect(computeETA('running', startedAt, 10, 10)).toBeNull()
    expect(computeETA('running', startedAt, 11, 10)).toBeNull()
  })

  it('elapsed < 1s → null (추정 부정확)', () => {
    const startedAt = new Date(fixedNow - 500).toISOString() // 0.5초 전
    expect(computeETA('running', startedAt, 1, 10)).toBeNull()
  })

  it('invalid date → null', () => {
    expect(computeETA('running', 'not-a-date', 5, 10)).toBeNull()
  })
})
