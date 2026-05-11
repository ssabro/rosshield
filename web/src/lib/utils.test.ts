import { describe, expect, it } from 'vitest'

import { cn, formatBytes } from './utils'

describe('cn', () => {
  it('merges Tailwind classes', () => {
    expect(cn('px-2', 'px-4')).toBe('px-4')
    expect(cn('text-sm', 'text-base')).toBe('text-base')
  })

  it('handles falsy values', () => {
    expect(cn('a', false, null, undefined, 'b')).toBe('a b')
  })
})

describe('formatBytes', () => {
  it('returns em-dash for invalid input', () => {
    expect(formatBytes(NaN)).toBe('—')
    expect(formatBytes(Infinity)).toBe('—')
    expect(formatBytes(-1)).toBe('—')
    expect(formatBytes(-1000)).toBe('—')
  })

  it('uses B for small values', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(1)).toBe('1 B')
    expect(formatBytes(1023)).toBe('1023 B')
  })

  it('switches to KB at 1024', () => {
    expect(formatBytes(1024)).toBe('1.0 KB')
    expect(formatBytes(1500)).toBe('1.5 KB')
    expect(formatBytes(102400)).toBe('100 KB') // 100+ → 정수 포맷
  })

  it('switches to MB at 1024 KB', () => {
    expect(formatBytes(1024 * 1024)).toBe('1.0 MB')
    expect(formatBytes(1500000)).toBe('1.4 MB')
  })

  it('switches to GB at 1024 MB', () => {
    expect(formatBytes(1024 * 1024 * 1024)).toBe('1.0 GB')
    expect(formatBytes(2.5 * 1024 * 1024 * 1024)).toBe('2.5 GB')
  })

  it('switches to TB at 1024 GB', () => {
    expect(formatBytes(1024 * 1024 * 1024 * 1024)).toBe('1.0 TB')
  })

  it('caps at TB unit (no PB)', () => {
    // 5 PB → 5120 TB로 표시 (units 배열 끝). 정수 포맷.
    expect(formatBytes(5 * 1024 * 1024 * 1024 * 1024 * 1024)).toBe('5120 TB')
  })

  it('uses 1 decimal for values < 100, integer for >= 100', () => {
    expect(formatBytes(99 * 1024)).toBe('99.0 KB')
    expect(formatBytes(100 * 1024)).toBe('100 KB')
    expect(formatBytes(999 * 1024)).toBe('999 KB')
  })
})
