import { afterEach, describe, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth'

import { requireRole } from './route-guards'

import type { User } from '@/stores/auth'

const baseUser = (roles?: string[]): User => ({
  id: 'u1',
  email: 'u@example.com',
  displayName: 'U',
  tenantId: 't1',
  roles,
})

afterEach(() => {
  useAuthStore.setState({ accessToken: null, user: null })
})

describe('requireRole', () => {
  it('passes when user has the allowed role', () => {
    useAuthStore.setState({ accessToken: 'tok', user: baseUser(['admin']) })
    expect(() => requireRole('admin')).not.toThrow()
  })

  it('passes when user has any of multiple allowed roles', () => {
    useAuthStore.setState({ accessToken: 'tok', user: baseUser(['auditor']) })
    expect(() => requireRole('admin', 'auditor')).not.toThrow()
  })

  it('throws redirect when user lacks the allowed role', () => {
    useAuthStore.setState({ accessToken: 'tok', user: baseUser(['operator']) })
    expect(() => requireRole('admin')).toThrow()
  })

  it('throws redirect when user.roles is undefined (legacy session)', () => {
    useAuthStore.setState({ accessToken: 'tok', user: baseUser(undefined) })
    expect(() => requireRole('admin')).toThrow()
  })

  it('throws redirect when user.roles is empty', () => {
    useAuthStore.setState({ accessToken: 'tok', user: baseUser([]) })
    expect(() => requireRole('admin')).toThrow()
  })

  it('throws redirect when user is null', () => {
    useAuthStore.setState({ accessToken: null, user: null })
    expect(() => requireRole('admin')).toThrow()
  })

  it('redirects to /overview', () => {
    useAuthStore.setState({ accessToken: 'tok', user: baseUser(['operator']) })
    let caught: unknown
    try {
      requireRole('admin')
    } catch (e) {
      caught = e
    }
    expect(caught).toBeDefined()
    // TanStack redirect throws an object whose options reference /overview.
    const obj = caught as { options?: { to?: string }; to?: string }
    const to = obj.options?.to ?? obj.to
    expect(to).toBe('/overview')
  })
})
