// E19.T1 — Compliance 페이지 helper 단위 테스트.
//
// "점수 게이지 렌더" — 본 페이지에서는 shadcn Badge variant + 퍼센트 문자열로 표현.
// 실 컴포넌트 마운트 대신 매핑 함수(scoreVariant·formatScore)를 export하여 직접 검증.

import { describe, expect, it } from 'vitest'

import {
  formatScore,
  groupByCategory,
  scoreVariant,
  statusBadgeVariant,
} from './compliance'

import type { ComplianceControlStatus } from '@/api/hooks'

describe('scoreVariant', () => {
  it('≥0.9 → default (높은 점수)', () => {
    expect(scoreVariant(1.0)).toBe('default')
    expect(scoreVariant(0.95)).toBe('default')
    expect(scoreVariant(0.9)).toBe('default')
  })

  it('0.7 ≤ score < 0.9 → secondary (중간 점수)', () => {
    expect(scoreVariant(0.89)).toBe('secondary')
    expect(scoreVariant(0.8)).toBe('secondary')
    expect(scoreVariant(0.7)).toBe('secondary')
  })

  it('< 0.7 → destructive (낮은 점수)', () => {
    expect(scoreVariant(0.69)).toBe('destructive')
    expect(scoreVariant(0.5)).toBe('destructive')
    expect(scoreVariant(0)).toBe('destructive')
  })
})

describe('formatScore', () => {
  it('1.0 → 100.0%', () => {
    expect(formatScore(1.0)).toBe('100.0%')
  })

  it('0.834 → 83.4% (소수점 1자리 반올림)', () => {
    expect(formatScore(0.834)).toBe('83.4%')
  })

  it('0.5 → 50.0%', () => {
    expect(formatScore(0.5)).toBe('50.0%')
  })

  it('0 → 0.0%', () => {
    expect(formatScore(0)).toBe('0.0%')
  })
})

describe('statusBadgeVariant', () => {
  it('pass → default', () => {
    expect(statusBadgeVariant('pass')).toBe('default')
  })
  it('fail → destructive', () => {
    expect(statusBadgeVariant('fail')).toBe('destructive')
  })
  it('partial → secondary', () => {
    expect(statusBadgeVariant('partial')).toBe('secondary')
  })
  it('not_applicable·unmapped·기타 → outline', () => {
    expect(statusBadgeVariant('not_applicable')).toBe('outline')
    expect(statusBadgeVariant('unmapped')).toBe('outline')
    expect(statusBadgeVariant('xxx')).toBe('outline')
  })
})

describe('groupByCategory', () => {
  const make = (id: string): ComplianceControlStatus => ({
    controlId: id,
    status: 'pass',
    passCount: 1,
    failCount: 0,
  })

  it('ISMS-P:1.1.1·1.1.2·1.2.1 → ISMS-P:1로 모두 묶임', () => {
    const out = groupByCategory([
      make('ISMS-P:1.1.1'),
      make('ISMS-P:1.1.2'),
      make('ISMS-P:1.2.1'),
    ])
    expect(Object.keys(out)).toEqual(['ISMS-P:1'])
    expect(out['ISMS-P:1']).toHaveLength(3)
  })

  it('서로 다른 prefix는 별도 그룹', () => {
    const out = groupByCategory([
      make('ISMS-P:1.1.1'),
      make('ISMS-P:2.1.1'),
      make('CIS-1.1.1.1'),
    ])
    expect(Object.keys(out).sort()).toEqual([
      'CIS-1',
      'ISMS-P:1',
      'ISMS-P:2',
    ])
  })

  it('점이 없는 ID는 자기 자신이 키', () => {
    const out = groupByCategory([make('FOO'), make('BAR')])
    expect(Object.keys(out).sort()).toEqual(['BAR', 'FOO'])
  })
})
