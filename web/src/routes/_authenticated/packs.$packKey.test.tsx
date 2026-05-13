// packs.$packKey.test.tsx — PackDetailPage helper 단위 테스트.
//
// route 파일은 createFileRoute 호출이라 RTL 직접 마운트 회피 — pure helper만 검증
// (findings/scans test 패턴).

import { describe, expect, it } from 'vitest'

import { filterChecksBySeverity } from './packs.$packKey'

import type { PackCheck } from '@/api/hooks'

const sample: PackCheck[] = [
  { id: 'ck_1', checkId: 'CIS-1.1.1.1', title: 'A', severity: 'high' },
  { id: 'ck_2', checkId: 'CIS-1.1.1.2', title: 'B', severity: 'medium' },
  { id: 'ck_3', checkId: 'CIS-1.1.1.3', title: 'C', severity: 'low' },
  { id: 'ck_4', checkId: 'CIS-2.1.1', title: 'D', severity: 'high' },
  { id: 'ck_5', checkId: 'CIS-3.1.1', title: 'E', severity: 'critical' },
] as PackCheck[]

describe('filterChecksBySeverity', () => {
  it('severity="" → 전체 반환', () => {
    expect(filterChecksBySeverity(sample, '')).toHaveLength(5)
  })

  it('severity="high" → 2건', () => {
    const r = filterChecksBySeverity(sample, 'high')
    expect(r).toHaveLength(2)
    expect(r.every((c) => c.severity === 'high')).toBe(true)
  })

  it('severity="medium" → 1건', () => {
    expect(filterChecksBySeverity(sample, 'medium')).toHaveLength(1)
  })

  it('severity="low" → 1건', () => {
    expect(filterChecksBySeverity(sample, 'low')).toHaveLength(1)
  })

  it('severity="critical" → 1건', () => {
    expect(filterChecksBySeverity(sample, 'critical')).toHaveLength(1)
  })

  it('severity="unknown" → 0건', () => {
    expect(filterChecksBySeverity(sample, 'unknown')).toHaveLength(0)
  })

  it('빈 input → 빈 결과', () => {
    expect(filterChecksBySeverity([], 'high')).toHaveLength(0)
    expect(filterChecksBySeverity([], '')).toHaveLength(0)
  })
})
