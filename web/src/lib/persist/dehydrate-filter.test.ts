// PWA persist Stage 2 — dehydrate filter 단위 테스트
// (design doc `pwa-persist-design.md` §3.5 + §6.2 + D-PWAPER-5).
//
// 검증 범위:
//  - 차단 list(deny prefix) 매치 시 false 반환 → IndexedDB 영속 차단.
//  - 통과 list(allow prefix) 매치 시 true 반환 → 정상 영속.
//  - 빈 queryKey · 비표준 queryKey도 안전 처리(throw 0).
//  - opt-out 정책 — 명시 차단되지 않은 모든 query는 default true (read 캐시 가치).

import { describe, expect, it } from 'vitest'

import {
  DENY_KEY_PREFIXES,
  shouldDehydrateQuery,
} from './dehydrate-filter'

// 최소 query stub — react-query Query는 풍부한 인스턴스이지만 우리 filter는
// `queryKey`만 참조. 테스트 격리를 위해 명시 stub 사용.
function stubQuery(queryKey: ReadonlyArray<unknown>) {
  return { queryKey } as unknown as Parameters<typeof shouldDehydrateQuery>[0]
}

describe('shouldDehydrateQuery — 보안 차단 list (D-PWAPER-5)', () => {
  // 차단 prefix는 design doc §3.5 + §6.2에 명시된 4종.
  // 새 차단 항목 추가 시 본 테스트도 함께 갱신해 회귀 차단.

  it('SSO query 차단 (clientSecret 누설 위험)', () => {
    expect(shouldDehydrateQuery(stubQuery(['sso']))).toBe(false)
    expect(shouldDehydrateQuery(stubQuery(['sso', 'providers']))).toBe(false)
    expect(
      shouldDehydrateQuery(stubQuery(['sso', 'providers', 'oidc-1'])),
    ).toBe(false)
  })

  it('webhook query 차단 (signing secret 누설 위험)', () => {
    expect(shouldDehydrateQuery(stubQuery(['webhooks']))).toBe(false)
    expect(shouldDehydrateQuery(stubQuery(['webhooks', 'wh-123']))).toBe(false)
  })

  it('invitation query 차단 (초대 token 누설 위험)', () => {
    expect(shouldDehydrateQuery(stubQuery(['invitations']))).toBe(false)
    expect(
      shouldDehydrateQuery(stubQuery(['invitations', 'token-abc'])),
    ).toBe(false)
  })

  it('advisor query 차단 (LLM 대화 사용자 입력 민감)', () => {
    expect(shouldDehydrateQuery(stubQuery(['advisor']))).toBe(false)
    expect(
      shouldDehydrateQuery(stubQuery(['advisor', 'conversations', 'c-1'])),
    ).toBe(false)
  })
})

describe('shouldDehydrateQuery — 통과 list (allow by default)', () => {
  // opt-out 정책 — 차단 list 외 모든 query는 영속 OK (D-PWAPER-5 채택 default).
  // 본 epic의 핵심 가치(read 캐시) 즉시 충족.

  it('robots query 영속 OK', () => {
    expect(shouldDehydrateQuery(stubQuery(['robots']))).toBe(true)
    expect(shouldDehydrateQuery(stubQuery(['robots', 'r-1']))).toBe(true)
  })

  it('scans query 영속 OK', () => {
    expect(shouldDehydrateQuery(stubQuery(['scans']))).toBe(true)
    expect(
      shouldDehydrateQuery(stubQuery(['scans', 's-1', 'findings'])),
    ).toBe(true)
  })

  it('fleets query 영속 OK', () => {
    expect(shouldDehydrateQuery(stubQuery(['fleets']))).toBe(true)
    expect(shouldDehydrateQuery(stubQuery(['fleets', 'f-1']))).toBe(true)
  })

  it('packs query 영속 OK', () => {
    expect(shouldDehydrateQuery(stubQuery(['packs']))).toBe(true)
    expect(
      shouldDehydrateQuery(stubQuery(['packs', 'p-1', 'checks'])),
    ).toBe(true)
  })

  it('me query 영속 OK', () => {
    expect(shouldDehydrateQuery(stubQuery(['me']))).toBe(true)
  })

  it('audit head query 영속 OK (공개 metadata 수준)', () => {
    expect(shouldDehydrateQuery(stubQuery(['audit', 'head']))).toBe(true)
  })

  it('license info query 영속 OK (deny list 외)', () => {
    // license는 design doc §2.5 표에서 민감도 "중" — 만료일·feature flag 정도.
    // SSO/webhook secret처럼 즉시 누설은 아니나, 향후 정책 변경 시 deny 추가 가능.
    expect(shouldDehydrateQuery(stubQuery(['license', 'info']))).toBe(true)
  })
})

describe('shouldDehydrateQuery — edge case', () => {
  it('빈 queryKey 통과 (deny prefix와 매치 안 됨)', () => {
    expect(shouldDehydrateQuery(stubQuery([]))).toBe(true)
  })

  it('차단 prefix와 동일 시작 단어이지만 string 다른 경우는 통과', () => {
    // 'sso-config' ≠ 'sso' (정확 일치 — startsWith 아님).
    expect(shouldDehydrateQuery(stubQuery(['sso-config']))).toBe(true)
    expect(shouldDehydrateQuery(stubQuery(['webhooks-history']))).toBe(true)
  })

  it('차단 prefix가 [0]이 아닌 위치에 있으면 통과 (prefix 정확 매치)', () => {
    // queryKey[0]만 검사 — 중간/끝에 차단 단어가 있어도 통과.
    expect(shouldDehydrateQuery(stubQuery(['robots', 'sso']))).toBe(true)
  })

  it('비-string queryKey[0]은 통과 (deny list는 string 비교)', () => {
    expect(shouldDehydrateQuery(stubQuery([42, 'data']))).toBe(true)
    expect(shouldDehydrateQuery(stubQuery([{ key: 'val' }]))).toBe(true)
    expect(shouldDehydrateQuery(stubQuery([null]))).toBe(true)
  })
})

describe('DENY_KEY_PREFIXES — export 무결성', () => {
  it('차단 list가 4종 (design doc §3.5 default)', () => {
    expect(DENY_KEY_PREFIXES).toHaveLength(4)
  })

  it('차단 list 항목 명세 (D-PWAPER-5 권장 default)', () => {
    expect(DENY_KEY_PREFIXES).toEqual(
      expect.arrayContaining(['sso', 'webhooks', 'invitations', 'advisor']),
    )
  })

  it('차단 list는 immutable (readonly)', () => {
    // 런타임 freeze 검증 — 실수로 push/mutation 차단.
    expect(Object.isFrozen(DENY_KEY_PREFIXES)).toBe(true)
  })
})
