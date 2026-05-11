// RBAC Stage 2-B — hasAnyRole 순수 함수 단위 테스트.
//
// useIsAdmin·useHasRole 등 React hook 자체는 useMe → React Query Provider 의존이라
// 회피. 핵심 분기 로직은 hasAnyRole에 분리되어 있어 본 파일에서 충분히 검증 가능.

import { describe, expect, it } from 'vitest'

import { hasAnyRole } from './hooks'

describe('hasAnyRole', () => {
  it('roles 중 하나라도 allowed에 포함되면 true', () => {
    expect(hasAnyRole(['admin'], ['admin'])).toBe(true)
    expect(hasAnyRole(['operator', 'admin'], ['admin'])).toBe(true)
    expect(hasAnyRole(['auditor'], ['admin', 'auditor'])).toBe(true)
  })

  it('교집합이 없으면 false', () => {
    expect(hasAnyRole(['operator'], ['admin'])).toBe(false)
    expect(hasAnyRole(['operator'], ['admin', 'auditor'])).toBe(false)
  })

  it('roles가 nil/undefined/빈 배열이면 false', () => {
    expect(hasAnyRole(undefined, ['admin'])).toBe(false)
    expect(hasAnyRole(null, ['admin'])).toBe(false)
    expect(hasAnyRole([], ['admin'])).toBe(false)
  })

  it('allowed가 빈 배열이면 false (안전 default)', () => {
    expect(hasAnyRole(['admin'], [])).toBe(false)
  })

  it('정확히 일치하는 케이스만 true (case sensitive)', () => {
    expect(hasAnyRole(['Admin'], ['admin'])).toBe(false)
    expect(hasAnyRole(['ADMIN'], ['admin'])).toBe(false)
  })
})
