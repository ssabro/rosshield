// D-UI-1 Stage 1 — severity·status helper 회귀.
//
// severityClassName·statusClassName이 design token alias (bg-severity-*, text-severity-*)
// 를 일관되게 노출하는지 검증. token 명을 하드코드하지 않고 helper만 import 하도록 강제하기 위해
// 모든 level이 entry를 갖는지·label key가 i18n dict와 동일 prefix를 사용하는지 확인.

import { describe, expect, it } from 'vitest'

import { ko, en } from '@/i18n/dict'
import {
  SEVERITY_LEVELS,
  STATUS_LEVELS,
  severityClassName,
  severityIcon,
  severityLabel,
  severityTextClassName,
  statusClassName,
  statusIcon,
  statusLabel,
  statusTextClassName,
} from './severity'

describe('severity helper', () => {
  it('모든 severity level이 5종 entry 보유', () => {
    expect(SEVERITY_LEVELS).toEqual([
      'critical',
      'high',
      'medium',
      'low',
      'info',
    ])
  })

  it('severityClassName은 severity- token utility 사용', () => {
    for (const level of SEVERITY_LEVELS) {
      expect(severityClassName[level]).toContain(`bg-severity-${level}`)
      expect(severityTextClassName[level]).toContain(`text-severity-${level}`)
    }
  })

  it('severityIcon은 모든 level에 Lucide icon 매핑', () => {
    for (const level of SEVERITY_LEVELS) {
      expect(severityIcon[level]).toBeDefined()
      expect(typeof severityIcon[level]).toBe('object')
    }
  })

  it('severityLabel key는 ko·en dict에 모두 존재', () => {
    for (const level of SEVERITY_LEVELS) {
      const key = severityLabel[level]
      expect(key).toBe(`severity.${level}`)
      expect(ko).toHaveProperty(key)
      expect(en).toHaveProperty(key)
    }
  })
})

describe('status helper', () => {
  it('모든 status level이 5종 entry 보유', () => {
    expect(STATUS_LEVELS).toEqual([
      'running',
      'pending',
      'completed',
      'failed',
      'cancelled',
    ])
  })

  it('statusClassName은 status- token utility 사용', () => {
    for (const level of STATUS_LEVELS) {
      expect(statusClassName[level]).toContain(`bg-status-${level}`)
      expect(statusTextClassName[level]).toContain(`text-status-${level}`)
    }
  })

  it('statusIcon은 모든 level에 Lucide icon 매핑', () => {
    for (const level of STATUS_LEVELS) {
      expect(statusIcon[level]).toBeDefined()
    }
  })

  it('statusLabel key는 ko·en dict에 모두 존재', () => {
    for (const level of STATUS_LEVELS) {
      const key = statusLabel[level]
      expect(key).toBe(`status.${level}`)
      expect(ko).toHaveProperty(key)
      expect(en).toHaveProperty(key)
    }
  })
})
