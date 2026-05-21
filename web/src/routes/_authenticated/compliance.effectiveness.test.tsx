// Phase 11.B-6 — `/compliance/effectiveness` 페이지 단위 테스트.
//
// useComplianceEffectiveness hook 을 mock 해서 페이지가 5+ state 를 분기 render 하는지
// 검증:
//   1. pending (skeleton)
//   2. success + 14 카테고리 + cover% 표시
//   3. empty (totalSubControls=0)
//   4. error 분기 (401/403 unauthorized · 503 unavailable · 그 외 fallback)
//
// 또 helper(coverVariant/coverThresholdKey/formatPercent/categoryNameKey) 단위 검증.

import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/api/errors'
import { useLocaleStore } from '@/i18n/store'

import type { ComplianceEffectivenessResponse } from '@/api/hooks'

interface EffStub {
  data: ComplianceEffectivenessResponse | undefined
  isPending: boolean
  isError: boolean
  isSuccess: boolean
  error: unknown
}

const PENDING_EFF: EffStub = {
  data: undefined,
  isPending: true,
  isError: false,
  isSuccess: false,
  error: null,
}

const SUCCESS_EFF: EffStub = {
  data: {
    totalSubControls: 40,
    coveredSubControls: 33,
    coverPercent: 82.5,
    generatedAt: '2026-05-21T12:00:00Z',
    categories: [
      {
        code: 'CC1',
        name: 'Control Environment',
        subControls: 5,
        covered: 3,
        coverPercent: 60.0,
        auditEvents: { lastDay: 0, last7Days: 5, last30Days: 18 },
        gaps: ['CC1.2 Board Oversight', 'CC1.4 Commitment to Competence'],
        items: [],
      },
      {
        code: 'CC6',
        name: 'Logical and Physical Access',
        subControls: 8,
        covered: 6,
        coverPercent: 75.0,
        auditEvents: { lastDay: 3, last7Days: 22, last30Days: 91 },
        gaps: ['CC6.4 Physical Access', 'CC6.5 Asset Disposal'],
        items: [],
      },
      {
        code: 'A1',
        name: 'Availability',
        subControls: 3,
        covered: 2,
        coverPercent: 66.7,
        auditEvents: { lastDay: 0, last7Days: 0, last30Days: 4 },
        gaps: ['A1.2 Environmental Protections'],
        items: [],
      },
    ],
  },
  isPending: false,
  isError: false,
  isSuccess: true,
  error: null,
}

const stubHolder = vi.hoisted(() => ({
  eff: {
    data: undefined,
    isPending: true,
    isError: false,
    isSuccess: false,
    error: null,
  } as EffStub,
}))

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => () => ({ component: () => null }),
}))

vi.mock('@/api/hooks', () => ({
  useComplianceEffectiveness: () => stubHolder.eff,
}))

// mock 호이스팅 후 import.
// eslint-disable-next-line import/first
import {
  ComplianceEffectivenessPage,
  categoryNameKey,
  coverThresholdKey,
  coverVariant,
  formatPercent,
} from './compliance.effectiveness'

function setEff(next: EffStub): void {
  stubHolder.eff = next
}

beforeEach(() => {
  // 한국어 dict 검증 안정화.
  useLocaleStore.getState().setLocale('ko')
})

afterEach(() => {
  setEff(PENDING_EFF)
})

describe('ComplianceEffectivenessPage', () => {
  it('pending → skeleton 표시', () => {
    setEff(PENDING_EFF)
    render(<ComplianceEffectivenessPage />)
    expect(screen.getAllByRole('status').length).toBeGreaterThanOrEqual(1)
  })

  it('success → cover% hero + 카테고리 매트릭스 + gaps 렌더', () => {
    setEff(SUCCESS_EFF)
    render(<ComplianceEffectivenessPage />)

    // hero — coverPercent 82.5% 노출. 매트릭스 헤더의 cover% 컬럼과 hero 모두 "82.5%"
    // 가 등장할 수 있어 getAll 로 검증.
    expect(screen.getAllByText('82.5%').length).toBeGreaterThanOrEqual(1)

    // 카테고리 코드 — CC1·CC6·A1 표시 (매트릭스 행 + gaps 카드 양쪽에서 등장 가능).
    expect(screen.getAllByText('CC1').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('CC6').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('A1').length).toBeGreaterThanOrEqual(1)

    // gaps 카드 — CC1.2 / CC1.4 / CC6.4 / CC6.5 / A1.2 listed (5 gaps total).
    expect(screen.getByText('CC1.2 Board Oversight')).toBeInTheDocument()
    expect(screen.getByText('A1.2 Environmental Protections')).toBeInTheDocument()
  })

  it('empty (totalSubControls=0) → empty state', () => {
    setEff({
      ...SUCCESS_EFF,
      data: {
        totalSubControls: 0,
        coveredSubControls: 0,
        coverPercent: 0,
        generatedAt: '',
        categories: [],
      },
    })
    render(<ComplianceEffectivenessPage />)
    expect(screen.getByText('매트릭스 비활성')).toBeInTheDocument()
  })

  it('error 401/403 → unauthorized 메시지', () => {
    setEff({
      data: undefined,
      isPending: false,
      isError: true,
      isSuccess: false,
      error: new ApiError(403, 'forbidden'),
    })
    render(<ComplianceEffectivenessPage />)
    expect(
      screen.getByText('권한이 부족합니다. auditor 또는 admin 역할이 필요합니다.'),
    ).toBeInTheDocument()
  })

  it('error 503 → unavailable 메시지', () => {
    setEff({
      data: undefined,
      isPending: false,
      isError: true,
      isSuccess: false,
      error: new ApiError(503, 'not configured'),
    })
    render(<ComplianceEffectivenessPage />)
    expect(
      screen.getByText('서버에서 effectiveness dashboard 가 비활성화되어 있습니다.'),
    ).toBeInTheDocument()
  })

  it('error non-ApiError → fallback 메시지', () => {
    setEff({
      data: undefined,
      isPending: false,
      isError: true,
      isSuccess: false,
      error: new Error('network down'),
    })
    render(<ComplianceEffectivenessPage />)
    expect(screen.getByText('서버 응답을 처리할 수 없습니다.')).toBeInTheDocument()
  })
})

describe('coverVariant', () => {
  it('≥ 90 → default (healthy)', () => {
    expect(coverVariant(95)).toBe('default')
    expect(coverVariant(90)).toBe('default')
  })
  it('70 ~ 89 → secondary (warning)', () => {
    expect(coverVariant(80)).toBe('secondary')
    expect(coverVariant(70)).toBe('secondary')
  })
  it('< 70 → destructive (critical)', () => {
    expect(coverVariant(50)).toBe('destructive')
    expect(coverVariant(0)).toBe('destructive')
  })
})

describe('coverThresholdKey', () => {
  it('≥ 90 → healthy', () => {
    expect(coverThresholdKey(99)).toBe('compliance.dashboard.threshold.healthy')
  })
  it('70 ~ 89 → warning', () => {
    expect(coverThresholdKey(75)).toBe('compliance.dashboard.threshold.warning')
  })
  it('< 70 → critical', () => {
    expect(coverThresholdKey(50)).toBe(
      'compliance.dashboard.threshold.critical',
    )
  })
})

describe('formatPercent', () => {
  it('1 자리 소수로 표시', () => {
    expect(formatPercent(82.5)).toBe('82.5%')
    expect(formatPercent(100)).toBe('100.0%')
    expect(formatPercent(0)).toBe('0.0%')
  })
})

describe('categoryNameKey', () => {
  it('알려진 카테고리는 i18n 키 반환', () => {
    expect(categoryNameKey('CC1')).toBe('compliance.dashboard.category.CC1')
    expect(categoryNameKey('A5')).toBe('compliance.dashboard.category.A5')
  })
  it('미지 카테고리는 undefined', () => {
    expect(categoryNameKey('A99')).toBeUndefined()
    expect(categoryNameKey('')).toBeUndefined()
  })
})
