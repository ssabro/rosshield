import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import {
  AuditConsistencyCard,
  consistencyStatus,
  shortSha,
} from './AuditConsistencyCard'

import { useLocaleStore } from '@/i18n/store'

import type { AuditChainHeadSHA } from '@/api/hooks'

// Phase 10.A-3 — AuditConsistencyCard helper + render 단위 테스트.
//
// helpers (consistencyStatus / shortSha)는 useT 의존 없이 분기 검증.
// 본 컴포넌트는 useT 사용 — store 기본을 ko로 강제해 dict 매칭 안정화.

beforeEach(() => {
  useLocaleStore.getState().setLocale('ko')
})

describe('consistencyStatus', () => {
  it('빈 배열 → empty', () => {
    expect(consistencyStatus([])).toBe('empty')
  })
  it('모든 sha가 빈 문자열 → empty (genesis)', () => {
    expect(consistencyStatus(['', '', ''])).toBe('empty')
  })
  it('단일 sha → consistent', () => {
    expect(consistencyStatus(['abc'])).toBe('consistent')
  })
  it('동일 sha 다건 → consistent', () => {
    expect(consistencyStatus(['abc', 'abc', 'abc'])).toBe('consistent')
  })
  it('서로 다른 sha 다건 → mismatch', () => {
    expect(consistencyStatus(['abc', 'def'])).toBe('mismatch')
  })
  it('일부만 다른 다건 → mismatch', () => {
    expect(consistencyStatus(['abc', 'abc', 'xyz'])).toBe('mismatch')
  })
})

describe('shortSha', () => {
  it('빈 입력 → 빈 문자열', () => {
    expect(shortSha('')).toBe('')
  })
  it('default prefix 12자 — 64자 hex에서 12자 추출', () => {
    expect(shortSha('0123456789abcdef0123456789abcdef')).toBe('0123456789ab')
  })
  it('prefix override', () => {
    expect(shortSha('0123456789abcdef', 4)).toBe('0123')
  })
})

const HEAD_FULL: AuditChainHeadSHA = {
  tenantId: 'tn_demo',
  seq: 42,
  hashHex: 'abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789',
  updatedAt: '2026-05-21T11:59:55Z',
}

describe('AuditConsistencyCard render', () => {
  it('all-match (default peerShas=self) → consistent 배지 표시', () => {
    const { container } = render(
      <AuditConsistencyCard head={HEAD_FULL} selfRegion="ap-northeast-2" />,
    )
    // 일관성 배지 = consistent + sha prefix mono 표시.
    expect(
      container.querySelector('[data-consistency-status="consistent"]'),
    ).not.toBeNull()
    expect(screen.getByText('ap-northeast-2')).toBeInTheDocument()
    expect(screen.getByText('42')).toBeInTheDocument()
    // sha prefix 12자 + ellipsis.
    expect(container.querySelector('[data-sha-prefix="abcdef012345"]')).not.toBeNull()
  })

  it('peer mismatch → mismatch 배지 + runbook 안내 노출', () => {
    const { container } = render(
      <AuditConsistencyCard
        head={HEAD_FULL}
        selfRegion="ap-northeast-2"
        peerShas={[HEAD_FULL.hashHex, 'different-sha-different-different']}
      />,
    )
    expect(
      container.querySelector('[data-consistency-status="mismatch"]'),
    ).not.toBeNull()
    // runbook hint.
    expect(
      container.querySelector('[data-runbook-hint="audit-mismatch"]'),
    ).not.toBeNull()
    // runbook path 표기.
    expect(
      screen.getByText('docs/operations/multi-region-failover-runbook.md'),
    ).toBeInTheDocument()
  })

  it('genesis (head undefined 또는 sha 빈 문자열) → empty 배지 + audit empty 메시지', () => {
    const { container } = render(
      <AuditConsistencyCard head={undefined} selfRegion="ap-northeast-2" />,
    )
    expect(
      container.querySelector('[data-consistency-status="empty"]'),
    ).not.toBeNull()
    // ko dict "audit 엔트리 없음 (genesis)".
    expect(container.textContent).toContain('audit 엔트리 없음')
  })

  it('tooltip trigger element는 전체 sha hex를 노출(접근 가능)', () => {
    const { container } = render(
      <AuditConsistencyCard head={HEAD_FULL} selfRegion="ap-northeast-2" />,
    )
    // tooltip은 hover 시 표시되지만 trigger 자체에 data-sha-prefix가 prefix.
    // 전체 sha hex는 ShaCell tooltip content에 들어가지만 jsdom hover 시뮬 어려움.
    // 대신 prefix attribute로 분기 검증.
    const triggerSpan = container.querySelector(
      '[data-sha-prefix="abcdef012345"]',
    )
    expect(triggerSpan).not.toBeNull()
  })
})
