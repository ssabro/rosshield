import { describe, expect, it } from 'vitest'

import {
  ROLE_ADMIN,
  ROLE_AUDITOR,
  ROLE_FLEET_ADMIN,
  ROLE_OPERATOR,
  ROLE_OWNER,
  ROLE_READ_ONLY,
  SystemRolePermissions,
  bindingsFromUser,
  decide,
  isTenantScopedRole,
} from './policy'

import type { Action, Resource, RoleBinding, Subject } from './policy'

// 본 테스트는 server `internal/platform/authz/decision_test.go` 의 일부 시나리오를
// TypeScript로 mirror — 매트릭스 결정 일관성 + scope 평가 + fallback 변환.

describe('decide — empty bindings', () => {
  it('빈 bindings → DENY', () => {
    const sub: Subject = { bindings: [] }
    expect(decide(sub, 'robot', 'read')).toBe(false)
  })

  it('null/undefined bindings → DENY', () => {
    expect(decide({ bindings: undefined as unknown as RoleBinding[] }, 'robot', 'read')).toBe(false)
  })
})

describe('decide — owner는 모든 (resource, action) 통과', () => {
  const owner: Subject = {
    bindings: [{ role: ROLE_OWNER, scopeType: 'tenant' }],
  }
  const allResources: Resource[] = [
    'robot',
    'scan',
    'report',
    'insight',
    'audit',
    'fleet',
    'compliance',
    'tenant_admin',
    'system',
  ]
  const allActions: Action[] = ['read', 'write', 'execute', 'admin', 'verify', 'export']

  for (const r of allResources) {
    for (const a of allActions) {
      it(`owner allows ${r}.${a}`, () => {
        expect(decide(owner, r, a)).toBe(true)
      })
    }
  }
})

describe('decide — admin tenant scope (모든 fleet implicit)', () => {
  const adm: Subject = {
    bindings: [{ role: ROLE_ADMIN, scopeType: 'tenant' }],
    fleetId: 'flt_x', // tenant scope는 fleetID 무관
  }

  it('admin은 robot.write 통과 (fleetID 무관)', () => {
    expect(decide(adm, 'robot', 'write')).toBe(true)
  })
  it('admin은 system.admin 통과', () => {
    expect(decide(adm, 'system', 'admin')).toBe(true)
  })
  it('admin은 audit.read 통과', () => {
    expect(decide(adm, 'audit', 'read')).toBe(true)
  })
  it('admin은 scan.write 미통과 (매트릭스에 없음)', () => {
    expect(decide(adm, 'scan', 'write')).toBe(false)
  })
})

describe('decide — fleet-admin 은 fleet scope 일치 시만 통과', () => {
  const fadm: Subject = {
    bindings: [{ role: ROLE_FLEET_ADMIN, scopeType: 'fleet', scopeId: 'flt_a' }],
    fleetId: 'flt_a',
  }

  it('fleet 일치 + robot.write → 통과', () => {
    expect(decide(fadm, 'robot', 'write')).toBe(true)
  })
  it('fleet 일치 + scan.execute → 통과', () => {
    expect(decide(fadm, 'scan', 'execute')).toBe(true)
  })
  it('fleet 미일치 → DENY', () => {
    const otherFleet: Subject = {
      bindings: [{ role: ROLE_FLEET_ADMIN, scopeType: 'fleet', scopeId: 'flt_a' }],
      fleetId: 'flt_b',
    }
    expect(decide(otherFleet, 'robot', 'write')).toBe(false)
  })
  it('fleet 컨텍스트 없음 (tenant 글로벌 요청) → DENY', () => {
    const noFleet: Subject = {
      bindings: [{ role: ROLE_FLEET_ADMIN, scopeType: 'fleet', scopeId: 'flt_a' }],
    }
    expect(decide(noFleet, 'robot', 'write')).toBe(false)
  })
  it('fleet-admin은 system.admin 미통과 (매트릭스에 없음)', () => {
    expect(decide(fadm, 'system', 'admin')).toBe(false)
  })
  it('fleet-admin은 audit.read 미통과 (tenant 권한)', () => {
    expect(decide(fadm, 'audit', 'read')).toBe(false)
  })
})

describe('decide — operator 는 fleet 한정 일상 운영', () => {
  const op: Subject = {
    bindings: [{ role: ROLE_OPERATOR, scopeType: 'fleet', scopeId: 'flt_a' }],
    fleetId: 'flt_a',
  }

  it('robot.write 통과', () => {
    expect(decide(op, 'robot', 'write')).toBe(true)
  })
  it('scan.execute 통과', () => {
    expect(decide(op, 'scan', 'execute')).toBe(true)
  })
  it('robot.admin 미통과 (operator는 admin 없음)', () => {
    expect(decide(op, 'robot', 'admin')).toBe(false)
  })
  it('insight.write 미통과 (operator는 read만)', () => {
    expect(decide(op, 'insight', 'write')).toBe(false)
  })
})

describe('decide — auditor 는 tenant 글로벌 read-only + verify/export', () => {
  const aud: Subject = {
    bindings: [{ role: ROLE_AUDITOR, scopeType: 'tenant' }],
  }

  it('audit.verify 통과', () => {
    expect(decide(aud, 'audit', 'verify')).toBe(true)
  })
  it('report.verify 통과', () => {
    expect(decide(aud, 'report', 'verify')).toBe(true)
  })
  it('robot.export 통과', () => {
    expect(decide(aud, 'robot', 'export')).toBe(true)
  })
  it('robot.write 미통과 (auditor는 write 0)', () => {
    expect(decide(aud, 'robot', 'write')).toBe(false)
  })
  it('tenant_admin.admin 미통과 (auditor는 sso/users 관리 0)', () => {
    expect(decide(aud, 'tenant_admin', 'admin')).toBe(false)
  })
})

describe('decide — read-only 는 read만', () => {
  const ro: Subject = {
    bindings: [{ role: ROLE_READ_ONLY, scopeType: 'tenant' }],
  }
  it('robot.read 통과', () => {
    expect(decide(ro, 'robot', 'read')).toBe(true)
  })
  it('robot.export 미통과 (auditor 묶음)', () => {
    expect(decide(ro, 'robot', 'export')).toBe(false)
  })
  it('audit.read 미통과 (auditor 묶음)', () => {
    expect(decide(ro, 'audit', 'read')).toBe(false)
  })
})

describe('decide — multi-binding (fleet + tenant 동시 보유)', () => {
  it('fleet[A] operator + tenant read-only — fleet[A] write 통과', () => {
    const sub: Subject = {
      bindings: [
        { role: ROLE_OPERATOR, scopeType: 'fleet', scopeId: 'flt_a' },
        { role: ROLE_READ_ONLY, scopeType: 'tenant' },
      ],
      fleetId: 'flt_a',
    }
    expect(decide(sub, 'robot', 'write')).toBe(true)
  })

  it('fleet[A] operator + tenant read-only — fleet[B] read 통과 (tenant scope)', () => {
    const sub: Subject = {
      bindings: [
        { role: ROLE_OPERATOR, scopeType: 'fleet', scopeId: 'flt_a' },
        { role: ROLE_READ_ONLY, scopeType: 'tenant' },
      ],
      fleetId: 'flt_b',
    }
    expect(decide(sub, 'robot', 'read')).toBe(true)
  })

  it('fleet[A] operator + tenant read-only — fleet[B] write DENY', () => {
    const sub: Subject = {
      bindings: [
        { role: ROLE_OPERATOR, scopeType: 'fleet', scopeId: 'flt_a' },
        { role: ROLE_READ_ONLY, scopeType: 'tenant' },
      ],
      fleetId: 'flt_b',
    }
    expect(decide(sub, 'robot', 'write')).toBe(false)
  })
})

describe('decide — 알려지지 않은 role은 무시', () => {
  it('custom-role binding은 skip, 다른 binding이 통과', () => {
    const sub: Subject = {
      bindings: [
        { role: 'custom-role', scopeType: 'tenant' },
        { role: ROLE_ADMIN, scopeType: 'tenant' },
      ],
    }
    expect(decide(sub, 'robot', 'write')).toBe(true)
  })

  it('custom-role 단독은 모두 DENY', () => {
    const sub: Subject = {
      bindings: [{ role: 'custom-role', scopeType: 'tenant' }],
    }
    expect(decide(sub, 'robot', 'read')).toBe(false)
  })
})

describe('decide — fleet scope 잘못된 binding (scopeId 누락)', () => {
  it('fleet scope이지만 scopeId 빈 문자열 → skip (DENY)', () => {
    const sub: Subject = {
      bindings: [{ role: ROLE_OPERATOR, scopeType: 'fleet', scopeId: '' }],
      fleetId: 'flt_a',
    }
    expect(decide(sub, 'robot', 'write')).toBe(false)
  })

  it('fleet scope이지만 scopeId undefined → skip (DENY)', () => {
    const sub: Subject = {
      bindings: [{ role: ROLE_OPERATOR, scopeType: 'fleet' }],
      fleetId: 'flt_a',
    }
    expect(decide(sub, 'robot', 'write')).toBe(false)
  })
})

describe('isTenantScopedRole', () => {
  it('owner/admin/auditor/read-only는 tenant scope', () => {
    expect(isTenantScopedRole(ROLE_OWNER)).toBe(true)
    expect(isTenantScopedRole(ROLE_ADMIN)).toBe(true)
    expect(isTenantScopedRole(ROLE_AUDITOR)).toBe(true)
    expect(isTenantScopedRole(ROLE_READ_ONLY)).toBe(true)
  })
  it('fleet-admin/operator는 fleet scope', () => {
    expect(isTenantScopedRole(ROLE_FLEET_ADMIN)).toBe(false)
    expect(isTenantScopedRole(ROLE_OPERATOR)).toBe(false)
  })
  it('알려지지 않은 role은 false', () => {
    expect(isTenantScopedRole('custom-role')).toBe(false)
  })
})

describe('bindingsFromUser — D-RBAC-7 호환 fallback', () => {
  it('user.bindings 있으면 그대로 사용', () => {
    const out = bindingsFromUser({
      roles: ['admin'],
      bindings: [{ role: ROLE_OPERATOR, scopeType: 'fleet', scopeId: 'flt_a' }],
    })
    expect(out).toHaveLength(1)
    expect(out[0]?.role).toBe(ROLE_OPERATOR)
    expect(out[0]?.scopeType).toBe('fleet')
    expect(out[0]?.scopeId).toBe('flt_a')
  })

  it('bindings 비어 있으면 roles를 모두 tenant scope로 fallback', () => {
    const out = bindingsFromUser({ roles: ['admin', 'auditor'] })
    expect(out).toHaveLength(2)
    expect(out.every((b) => b.scopeType === 'tenant')).toBe(true)
    expect(out.map((b) => b.role).sort()).toEqual(['admin', 'auditor'])
  })

  it('roles + bindings 모두 비면 빈 슬라이스', () => {
    expect(bindingsFromUser({})).toEqual([])
    expect(bindingsFromUser({ roles: [] })).toEqual([])
    expect(bindingsFromUser({ roles: null })).toEqual([])
  })
})

describe('SystemRolePermissions matrix shape', () => {
  it('owner는 단일 wildcard permission만 보유', () => {
    const perms = SystemRolePermissions[ROLE_OWNER]
    expect(perms).toHaveLength(1)
    expect(perms?.[0]).toEqual({ resource: '*', action: '*' })
  })

  it('6개 시스템 role 모두 정의됨', () => {
    for (const r of [
      ROLE_OWNER,
      ROLE_ADMIN,
      ROLE_FLEET_ADMIN,
      ROLE_OPERATOR,
      ROLE_AUDITOR,
      ROLE_READ_ONLY,
    ]) {
      expect(SystemRolePermissions[r]).toBeDefined()
    }
  })

  it('알려지지 않은 role은 undefined', () => {
    expect(SystemRolePermissions['custom-role']).toBeUndefined()
  })
})
