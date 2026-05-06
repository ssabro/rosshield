// E19.T2 — Findings 페이지 helper 단위 테스트.
//
// route 파일은 createFileRoute를 호출하므로 RTL로 직접 마운트하지 않고, 페이지에서
// export한 pure helper(severityVariant·buildInsightsFilter)만 검증한다 (login.test.tsx 패턴).

import { describe, expect, it } from 'vitest'

import { buildInsightsFilter, severityVariant } from './findings'

describe('severityVariant', () => {
  it('critical/high → destructive', () => {
    expect(severityVariant('critical')).toBe('destructive')
    expect(severityVariant('high')).toBe('destructive')
  })

  it('medium → default', () => {
    expect(severityVariant('medium')).toBe('default')
  })

  it('low/info → secondary', () => {
    expect(severityVariant('low')).toBe('secondary')
    expect(severityVariant('info')).toBe('secondary')
  })

  it('알 수 없는 값은 secondary fallback', () => {
    expect(severityVariant('unknown')).toBe('secondary')
    expect(severityVariant('')).toBe('secondary')
  })
})

describe('buildInsightsFilter', () => {
  it('모든 필드가 빈 값이면 빈 객체 반환', () => {
    expect(buildInsightsFilter({ kind: '', severity: '', robotId: '' })).toEqual({})
  })

  it('kind만 설정되면 kind만 포함', () => {
    expect(buildInsightsFilter({ kind: 'drift', severity: '', robotId: '' })).toEqual({
      kind: 'drift',
    })
  })

  it('severity만 설정되면 severity만 포함', () => {
    expect(buildInsightsFilter({ kind: '', severity: 'high', robotId: '' })).toEqual({
      severity: 'high',
    })
  })

  it('robotId는 trim 후 비어있으면 제외', () => {
    expect(buildInsightsFilter({ kind: '', severity: '', robotId: '   ' })).toEqual({})
    expect(buildInsightsFilter({ kind: '', severity: '', robotId: '  ro_X  ' })).toEqual({
      robotId: 'ro_X',
    })
  })

  it('3개 필드가 모두 채워지면 3개 모두 포함', () => {
    expect(
      buildInsightsFilter({ kind: 'anomaly', severity: 'medium', robotId: 'ro_AB' }),
    ).toEqual({
      kind: 'anomaly',
      severity: 'medium',
      robotId: 'ro_AB',
    })
  })
})
