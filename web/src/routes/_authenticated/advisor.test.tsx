// E19.T3 — Advisor 페이지 helper 단위 테스트.
//
// backlog는 "LLM disabled 시 페이지 숨김"으로 명시되었지만 구현은 페이지를 항상 노출하고
// 503 응답 시 안내 박스를 보여주는 방식으로 채택됨 (일관된 UX). 본 테스트는 그 안내 메시지
// 분기와 turn role → Badge variant 매핑을 검증한다.

import { describe, expect, it } from 'vitest'

import { ApiError } from '@/api/errors'

import { resolveAskErrorMessage, roleVariant } from './advisor'

describe('roleVariant', () => {
  it('user → default', () => {
    expect(roleVariant('user')).toBe('default')
  })

  it('assistant → secondary', () => {
    expect(roleVariant('assistant')).toBe('secondary')
  })

  it('tool → outline', () => {
    expect(roleVariant('tool')).toBe('outline')
  })

  it('알 수 없는 role → outline fallback', () => {
    expect(roleVariant('system')).toBe('outline')
    expect(roleVariant('unknown')).toBe('outline')
  })
})

describe('resolveAskErrorMessage', () => {
  it('ApiError 503 → 옵트인 활성화 안내 (LLM disabled)', () => {
    const err = new ApiError(503, 'service unavailable')
    expect(resolveAskErrorMessage(err)).toMatch(/Advisor가 비활성 상태/)
    expect(resolveAskErrorMessage(err)).toMatch(/--llm-provider/)
  })

  it('ApiError 그 외 status → 서버 메시지 노출', () => {
    const err = new ApiError(401, '인증 만료됨')
    expect(resolveAskErrorMessage(err)).toBe('인증 만료됨')
  })

  it('ApiError 400 → 서버 메시지 노출 (empty question 등)', () => {
    const err = new ApiError(400, 'advisor: question is required')
    expect(resolveAskErrorMessage(err)).toBe('advisor: question is required')
  })

  it('비-ApiError → 일반 fallback 메시지', () => {
    expect(resolveAskErrorMessage(new Error('네트워크 단절'))).toBe(
      '질문 처리 중 오류가 발생했습니다',
    )
    expect(resolveAskErrorMessage(undefined)).toBe('질문 처리 중 오류가 발생했습니다')
    expect(resolveAskErrorMessage(null)).toBe('질문 처리 중 오류가 발생했습니다')
  })
})
