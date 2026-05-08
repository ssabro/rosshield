// B2 — `/invitations/accept/{token}` 페이지 helper 단위 테스트.
//
// 페이지 마운트는 TanStack Router 의존이라 회피 — helper export만 검증.

import { describe, expect, it } from 'vitest'

import {
  invitationPreviewState,
  validateAcceptForm,
} from './invitations.accept.$token'

describe('invitationPreviewState', () => {
  it('accepted=true → used', () => {
    expect(
      invitationPreviewState({
        expiresAt: '2099-01-01T00:00:00Z',
        accepted: true,
      }),
    ).toBe('used')
  })
  it('expiresAt 과거 → expired', () => {
    expect(
      invitationPreviewState({
        expiresAt: '2000-01-01T00:00:00Z',
        accepted: false,
      }),
    ).toBe('expired')
  })
  it('미수락 + 만료 전 → active', () => {
    expect(
      invitationPreviewState({
        expiresAt: '2099-01-01T00:00:00Z',
        accepted: false,
      }),
    ).toBe('active')
  })
  it('used가 expired보다 우선', () => {
    expect(
      invitationPreviewState({
        expiresAt: '2000-01-01T00:00:00Z',
        accepted: true,
      }),
    ).toBe('used')
  })
})

describe('validateAcceptForm', () => {
  it('모두 유효하면 빈 배열', () => {
    expect(
      validateAcceptForm({
        email: 'alice@example.com',
        displayName: 'Alice',
        password: 'verysecretpass1',
      }),
    ).toEqual([])
  })
  it('email 빈 값은 email 에러', () => {
    expect(
      validateAcceptForm({
        email: '',
        displayName: 'Alice',
        password: 'verysecretpass1',
      }),
    ).toContain('email')
  })
  it('displayName 빈 값은 displayName 에러', () => {
    expect(
      validateAcceptForm({
        email: 'alice@example.com',
        displayName: '',
        password: 'verysecretpass1',
      }),
    ).toContain('displayName')
  })
  it('password가 12자 미만이면 password 에러', () => {
    expect(
      validateAcceptForm({
        email: 'alice@example.com',
        displayName: 'Alice',
        password: 'short',
      }),
    ).toContain('password')
  })
  it('공백만 있는 displayName도 에러', () => {
    expect(
      validateAcceptForm({
        email: 'alice@example.com',
        displayName: '   ',
        password: 'verysecretpass1',
      }),
    ).toContain('displayName')
  })
})
