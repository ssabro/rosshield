// auth 스토어 단위 테스트.
//
// C6 — refresh token은 store에서 제거됨 (HttpOnly cookie 관리). access + user만 영속.
import { beforeEach, describe, expect, it } from 'vitest'

import { useAuthStore } from './auth'

describe('useAuthStore', () => {
  beforeEach(() => {
    useAuthStore.getState().clearSession()
    localStorage.clear()
  })

  it('starts empty', () => {
    const s = useAuthStore.getState()
    expect(s.accessToken).toBeNull()
    expect(s.user).toBeNull()
  })

  it('setSession populates accessToken + user', () => {
    useAuthStore.getState().setSession({
      accessToken: 'at_x',
      user: { id: 'us_1', email: 'a@b.c', displayName: 'A', tenantId: 'tn_1' },
    })
    const s = useAuthStore.getState()
    expect(s.accessToken).toBe('at_x')
    expect(s.user?.email).toBe('a@b.c')
  })

  it('setAccessToken updates token without touching user', () => {
    useAuthStore.getState().setSession({
      accessToken: 'old',
      user: { id: '1', email: 'x', displayName: 'X', tenantId: 't' },
    })
    useAuthStore.getState().setAccessToken('new')
    const s = useAuthStore.getState()
    expect(s.accessToken).toBe('new')
    expect(s.user?.id).toBe('1')
  })

  it('clearSession resets to nulls', () => {
    useAuthStore.getState().setSession({
      accessToken: 'at',
      user: { id: '1', email: 'x', displayName: 'X', tenantId: 't' },
    })
    useAuthStore.getState().clearSession()
    const s = useAuthStore.getState()
    expect(s.accessToken).toBeNull()
    expect(s.user).toBeNull()
  })

  it("persists accessToken/user to localStorage under 'rosshield-auth'", () => {
    useAuthStore.getState().setSession({
      accessToken: 'persist-me',
      user: { id: '1', email: 'x', displayName: 'X', tenantId: 't' },
    })
    const raw = localStorage.getItem('rosshield-auth')
    expect(raw).toBeTruthy()
    expect(raw).toContain('persist-me')
    // refreshToken·기타 필드 부재 보장 (legacy localStorage 잔여 필드도 partialize에 의해 hydrate에서 drop).
    expect(raw).not.toContain('refreshToken')
  })
})
