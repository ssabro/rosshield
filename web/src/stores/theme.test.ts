// 폴리시 2차 — theme store helper 단위 테스트.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { applyTheme, resolveDark } from './theme'

// jsdom은 matchMedia를 노출하지 않으므로 globalThis.window에 직접 stub.
const stubMatchMedia = (matches: boolean): void => {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: vi.fn(() => ({
      matches,
      media: '',
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  })
}

beforeEach(() => {
  document.documentElement.classList.remove('dark')
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('resolveDark', () => {
  it('light → false', () => {
    expect(resolveDark('light')).toBe(false)
  })
  it('dark → true', () => {
    expect(resolveDark('dark')).toBe(true)
  })
  it('system + prefers-dark → true', () => {
    stubMatchMedia(true)
    expect(resolveDark('system')).toBe(true)
  })
  it('system + prefers-light → false', () => {
    stubMatchMedia(false)
    expect(resolveDark('system')).toBe(false)
  })
})

describe('applyTheme', () => {
  it('dark → documentElement에 .dark 추가', () => {
    applyTheme('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('light → .dark 제거', () => {
    document.documentElement.classList.add('dark')
    applyTheme('light')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('system + prefers-dark → .dark 추가', () => {
    stubMatchMedia(true)
    applyTheme('system')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})
