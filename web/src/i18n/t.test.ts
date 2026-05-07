// C5 i18n — translate/nextLocale helper 단위 테스트.
// useT/useLocaleStore는 React/zustand 의존이라 별도 컴포넌트 테스트에서 검증.

import { describe, expect, it } from 'vitest'

import { translate } from './t'
import { nextLocale } from './store'

describe('translate', () => {
  it('ko 사전에서 키를 그대로 반환', () => {
    expect(translate('ko', 'nav.robots')).toBe('로봇')
    expect(translate('ko', 'login.submit')).toBe('로그인')
  })

  it('en 사전은 영어 값 반환', () => {
    expect(translate('en', 'nav.robots')).toBe('Robots')
    expect(translate('en', 'login.submit')).toBe('Sign in')
  })

  it('vars 보간 — {label} 치환', () => {
    expect(
      translate('ko', 'header.theme.tooltip', { label: '다크' }),
    ).toBe('테마: 다크 (클릭으로 전환)')
    expect(
      translate('en', 'header.theme.tooltip', { label: 'Dark' }),
    ).toBe('Theme: Dark (click to cycle)')
  })

  it('숫자 vars도 toString되어 치환됨', () => {
    // 동일 placeholder가 두 번 등장하는 키는 없으므로 단순 1회 치환만 검증.
    expect(
      translate('ko', 'header.locale.tooltip', { label: 1 as unknown as string }),
    ).toContain('1')
  })

  it('미존재 키는 [missing:key] 표시', () => {
    expect(translate('ko', 'unknown.key' as never)).toBe('[missing:unknown.key]')
  })
})

describe('nextLocale', () => {
  it('ko → en, en → ko 사이클', () => {
    expect(nextLocale('ko')).toBe('en')
    expect(nextLocale('en')).toBe('ko')
  })
})
