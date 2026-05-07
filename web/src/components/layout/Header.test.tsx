// 폴리시 2차 — Header의 nextTheme 사이클 단위 테스트.

import { describe, expect, it } from 'vitest'

import { nextTheme } from './Header'

describe('nextTheme', () => {
  it('light → dark', () => {
    expect(nextTheme('light')).toBe('dark')
  })
  it('dark → system', () => {
    expect(nextTheme('dark')).toBe('system')
  })
  it('system → light', () => {
    expect(nextTheme('system')).toBe('light')
  })
})
