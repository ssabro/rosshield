// B2 — `/users` 페이지 helper 단위 테스트.
//
// 페이지 마운트 자체는 TanStack Router 의존이라 회피 — 다른 페이지(integrations·sso)
// 와 동일한 패턴으로 helper export 함수만 검증.

import { describe, expect, it } from 'vitest'

import {
  acceptUrl,
  invitationStatus,
  invitationStatusLabelKey,
  statusBadgeVariant,
} from './users'

import type { InvitationView } from '@/api/hooks'

const baseInvitation: InvitationView = {
  id: 'inv_01',
  email: 'alice@example.com',
  roleName: 'admin',
  invitedBy: 'usr_admin',
  expiresAt: '2099-01-01T00:00:00Z',
  createdAt: '2026-05-01T00:00:00Z',
}

describe('invitationStatus', () => {
  it('acceptedAt이 있으면 accepted', () => {
    expect(
      invitationStatus({
        ...baseInvitation,
        acceptedAt: '2026-05-02T00:00:00Z',
      }),
    ).toBe('accepted')
  })
  it('expiresAt이 과거면 expired', () => {
    expect(
      invitationStatus({
        ...baseInvitation,
        expiresAt: '2000-01-01T00:00:00Z',
      }),
    ).toBe('expired')
  })
  it('미수락 + 만료 전이면 pending', () => {
    expect(invitationStatus(baseInvitation)).toBe('pending')
  })
  it('accepted가 expired보다 우선', () => {
    expect(
      invitationStatus({
        ...baseInvitation,
        expiresAt: '2000-01-01T00:00:00Z',
        acceptedAt: '2000-01-02T00:00:00Z',
      }),
    ).toBe('accepted')
  })
})

describe('statusBadgeVariant', () => {
  it('pending → default', () => {
    expect(statusBadgeVariant('pending')).toBe('default')
  })
  it('accepted → secondary', () => {
    expect(statusBadgeVariant('accepted')).toBe('secondary')
  })
  it('expired → outline', () => {
    expect(statusBadgeVariant('expired')).toBe('outline')
  })
})

describe('invitationStatusLabelKey', () => {
  it('각 status에 대응되는 dict 키 반환', () => {
    expect(invitationStatusLabelKey('pending')).toBe(
      'users.status.pending',
    )
    expect(invitationStatusLabelKey('accepted')).toBe(
      'users.status.accepted',
    )
    expect(invitationStatusLabelKey('expired')).toBe(
      'users.status.expired',
    )
  })
})

describe('acceptUrl', () => {
  it('현재 origin 기반 /invitations/accept/{token} URL을 만든다', () => {
    const origin = 'https://app.example.com'
    expect(acceptUrl('tok_ABC', origin)).toBe(
      'https://app.example.com/invitations/accept/tok_ABC',
    )
  })
  it('token이 URL-safe하지 않은 문자를 포함하면 encode', () => {
    const origin = 'https://app.example.com'
    expect(acceptUrl('a/b c', origin)).toBe(
      'https://app.example.com/invitations/accept/a%2Fb%20c',
    )
  })
  it('origin 끝의 슬래시는 정리', () => {
    expect(acceptUrl('tok_X', 'https://app.example.com/')).toBe(
      'https://app.example.com/invitations/accept/tok_X',
    )
  })
})
