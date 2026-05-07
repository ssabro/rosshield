// C5 i18n — ko/en 사전이 동일한 키 셋을 제공하는지 검증.
// 누락 시 사용자가 한 언어 전환만으로 [missing:key] 폴백을 보게 되므로 강제.

import { describe, expect, it } from 'vitest'

import { en, ko } from './dict'

describe('dict 키 동기화', () => {
  it('ko와 en이 같은 키 셋을 노출', () => {
    const koKeys = Object.keys(ko).sort()
    const enKeys = Object.keys(en).sort()
    expect(enKeys).toEqual(koKeys)
  })

  it('모든 값이 비어있지 않은 string', () => {
    for (const [k, v] of Object.entries(ko)) {
      expect(typeof v).toBe('string')
      expect(v.length, `ko[${k}] empty`).toBeGreaterThan(0)
    }
    for (const [k, v] of Object.entries(en)) {
      expect(typeof v).toBe('string')
      expect(v.length, `en[${k}] empty`).toBeGreaterThan(0)
    }
  })
})
