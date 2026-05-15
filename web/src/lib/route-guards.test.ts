import { afterEach, describe, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth'

import { requirePermission, requireRole } from './route-guards'

import type { RoleBindingDTO, User } from '@/stores/auth'

const baseUser = (roles?: string[], bindings?: RoleBindingDTO[]): User => ({
  id: 'u1',
  email: 'u@example.com',
  displayName: 'U',
  tenantId: 't1',
  roles,
  bindings,
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

describe('requirePermission — RBAC Stage 5', () => {
  it('admin role (tenant fallback) — robot.write 통과', () => {
    useAuthStore.setState({ accessToken: 'tok', user: baseUser(['admin']) })
    expect(() => requirePermission('robot', 'write')).not.toThrow()
  })

  it('admin role — tenant_admin.admin 통과 (sso/users 페이지)', () => {
    useAuthStore.setState({ accessToken: 'tok', user: baseUser(['admin']) })
    expect(() => requirePermission('tenant_admin', 'admin')).not.toThrow()
  })

  it('auditor role — system.read 통과 (system 운영 페이지)', () => {
    useAuthStore.setState({ accessToken: 'tok', user: baseUser(['auditor']) })
    expect(() => requirePermission('system', 'read')).not.toThrow()
  })

  it('operator role (tenant fallback) — robot.write 통과 (legacy roles fallback)', () => {
    // D-RBAC-7 호환: bindings 부재 시 roles는 모두 tenant scope로 fallback.
    // operator는 tenant scope면 모든 fleet implicit (legacy 호환 정책).
    useAuthStore.setState({ accessToken: 'tok', user: baseUser(['operator']) })
    expect(() => requirePermission('robot', 'write')).not.toThrow()
  })

  it('operator with fleet binding — fleet 일치 robot.write 통과', () => {
    useAuthStore.setState({
      accessToken: 'tok',
      user: baseUser(undefined, [
        { role: 'operator', scopeType: 'fleet', scopeId: 'flt_a' },
      ]),
    })
    expect(() => requirePermission('robot', 'write', 'flt_a')).not.toThrow()
  })

  it('operator with fleet binding — fleet 미일치 → redirect', () => {
    useAuthStore.setState({
      accessToken: 'tok',
      user: baseUser(undefined, [
        { role: 'operator', scopeType: 'fleet', scopeId: 'flt_a' },
      ]),
    })
    expect(() => requirePermission('robot', 'write', 'flt_b')).toThrow()
  })

  it('user 없음 → redirect', () => {
    useAuthStore.setState({ accessToken: null, user: null })
    expect(() => requirePermission('robot', 'read')).toThrow()
  })

  it('roles 빈 셋 → redirect', () => {
    useAuthStore.setState({ accessToken: 'tok', user: baseUser([]) })
    expect(() => requirePermission('robot', 'read')).toThrow()
  })

  it('read-only role — robot.write 미통과', () => {
    useAuthStore.setState({ accessToken: 'tok', user: baseUser(['read-only']) })
    expect(() => requirePermission('robot', 'write')).toThrow()
  })

  it('redirect는 /overview', () => {
    useAuthStore.setState({ accessToken: 'tok', user: baseUser(['read-only']) })
    let caught: unknown
    try {
      requirePermission('robot', 'write')
    } catch (e) {
      caught = e
    }
    const obj = caught as { options?: { to?: string }; to?: string }
    const to = obj?.options?.to ?? obj?.to
    expect(to).toBe('/overview')
  })
})
