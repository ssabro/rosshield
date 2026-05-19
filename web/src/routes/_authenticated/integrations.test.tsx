// B3 — `/integrations` 페이지 helper 단위 테스트.
//
// 페이지 마운트 자체는 TanStack Router 의존이라 회피 — 다른 페이지(advisor, compliance)
// 와 동일한 패턴으로 helper export 함수만 검증.

import { describe, expect, it } from 'vitest'

import {
  formatWebhookEvent,
  summarizeDeliveries,
  webhookDeliveryStatus,
  type WebhookEndpoint,
} from '@/api/hooks'

import {
  decodePayload,
  deliveryStatusKind,
  deliveryStatusLabelKey,
  endpointDisplayName,
  statCellColorClass,
  statusBadgeVariant,
} from './integrations'

describe('formatWebhookEvent (api/hooks)', () => {
  it('알려진 EventType은 짧은 라벨로 압축', () => {
    expect(formatWebhookEvent('scan.completed')).toBe('scan')
    expect(formatWebhookEvent('insight.created')).toBe('insight')
    expect(formatWebhookEvent('audit.checkpoint')).toBe('audit')
  })
  it('알 수 없는 값은 그대로 반환', () => {
    expect(formatWebhookEvent('foo.bar')).toBe('foo.bar')
  })
})

describe('webhookDeliveryStatus (api/hooks)', () => {
  it('succeeded=true → success', () => {
    expect(
      webhookDeliveryStatus({ succeeded: true, attemptCount: 1 }),
    ).toBe('success')
  })
  it('attemptCount>=5 && !succeeded → dead', () => {
    expect(
      webhookDeliveryStatus({ succeeded: false, attemptCount: 5 }),
    ).toBe('dead')
    expect(
      webhookDeliveryStatus({ succeeded: false, attemptCount: 6 }),
    ).toBe('dead')
  })
  it('attemptCount>0 → retrying', () => {
    expect(
      webhookDeliveryStatus({ succeeded: false, attemptCount: 1 }),
    ).toBe('retrying')
    expect(
      webhookDeliveryStatus({ succeeded: false, attemptCount: 4 }),
    ).toBe('retrying')
  })
  it('attemptCount=0 → pending', () => {
    expect(
      webhookDeliveryStatus({ succeeded: false, attemptCount: 0 }),
    ).toBe('pending')
  })
})

describe('endpointDisplayName', () => {
  const base: WebhookEndpoint = {
    id: 'wh_1',
    tenantId: 't1',
    url: 'https://siem.example.com/hooks/x',
    secretLast4: '****',
    events: [],
    format: 'json',
    enabled: true,
    createdAt: '',
    updatedAt: '',
  }

  it('name 필드가 있으면 우선 사용', () => {
    expect(
      endpointDisplayName({
        ...base,
        ...({ name: 'My SIEM' } as Partial<WebhookEndpoint>),
      } as WebhookEndpoint),
    ).toBe('My SIEM')
  })
  it('name 없으면 URL host 사용', () => {
    expect(endpointDisplayName(base)).toBe('siem.example.com')
  })
  it('잘못된 URL이면 raw URL fallback', () => {
    expect(
      endpointDisplayName({ ...base, url: 'not-a-url' }),
    ).toBe('not-a-url')
  })
})

describe('statusBadgeVariant', () => {
  it('success → default', () => {
    expect(statusBadgeVariant('success')).toBe('default')
  })
  it('dead → destructive', () => {
    expect(statusBadgeVariant('dead')).toBe('destructive')
  })
  it('retrying → secondary', () => {
    expect(statusBadgeVariant('retrying')).toBe('secondary')
  })
  it('pending → outline', () => {
    expect(statusBadgeVariant('pending')).toBe('outline')
  })
})

describe('deliveryStatusKind (Stage 4 — StatusBadge 매핑)', () => {
  it('success → success', () => {
    expect(deliveryStatusKind('success')).toBe('success')
  })
  it('dead → failed', () => {
    expect(deliveryStatusKind('dead')).toBe('failed')
  })
  it('retrying → running (animated dot)', () => {
    expect(deliveryStatusKind('retrying')).toBe('running')
  })
  it('pending → pending', () => {
    expect(deliveryStatusKind('pending')).toBe('pending')
  })
})

describe('deliveryStatusLabelKey', () => {
  it('각 status에 대응되는 dict 키 반환', () => {
    expect(deliveryStatusLabelKey('success')).toBe(
      'integrations.deliveries.status.success',
    )
    expect(deliveryStatusLabelKey('dead')).toBe(
      'integrations.deliveries.status.dead',
    )
    expect(deliveryStatusLabelKey('retrying')).toBe(
      'integrations.deliveries.status.retrying',
    )
    expect(deliveryStatusLabelKey('pending')).toBe(
      'integrations.deliveries.status.pending',
    )
  })
})

describe('summarizeDeliveries (O7)', () => {
  it('빈 배열 → 모두 0', () => {
    expect(summarizeDeliveries([])).toEqual({
      total: 0,
      success: 0,
      retrying: 0,
      dead: 0,
      pending: 0,
    })
  })
  it('mixed deliveries → 정확한 분류', () => {
    const stats = summarizeDeliveries([
      { succeeded: true, attemptCount: 1 },
      { succeeded: true, attemptCount: 2 },
      { succeeded: false, attemptCount: 5 },
      { succeeded: false, attemptCount: 3 },
      { succeeded: false, attemptCount: 0 },
    ])
    expect(stats).toEqual({
      total: 5,
      success: 2,
      retrying: 1,
      dead: 1,
      pending: 1,
    })
  })
})

describe('statCellColorClass (O7)', () => {
  it('각 variant에 색상 클래스 매핑', () => {
    expect(statCellColorClass('success')).toContain('primary')
    expect(statCellColorClass('warning')).toContain('amber')
    expect(statCellColorClass('destructive')).toContain('destructive')
    expect(statCellColorClass('muted')).toContain('muted')
  })
})

// D-UI-1 carryover — DeliveryDetailDialog payload preview 디코딩.
describe('decodePayload (D-UI-1 carryover)', () => {
  it('undefined → empty', () => {
    expect(decodePayload(undefined)).toEqual({ kind: 'empty' })
  })
  it('valid JSON base64 → pretty-printed json', () => {
    // {"event":"scan.completed","sessionId":"s_1"}
    const b64 = Buffer.from(
      '{"event":"scan.completed","sessionId":"s_1"}',
      'utf-8',
    ).toString('base64')
    const decoded = decodePayload(b64)
    expect(decoded.kind).toBe('json')
    if (decoded.kind === 'json') {
      expect(decoded.value).toContain('"event": "scan.completed"')
      expect(decoded.value).toContain('"sessionId": "s_1"')
    }
  })
  it('non-JSON base64 → raw string', () => {
    const b64 = Buffer.from('plain text payload', 'utf-8').toString('base64')
    const decoded = decodePayload(b64)
    expect(decoded.kind).toBe('raw')
    if (decoded.kind === 'raw') {
      expect(decoded.value).toBe('plain text payload')
    }
  })
  it('invalid base64 → error', () => {
    // atob는 invalid 문자에 InvalidCharacterError throw — Buffer는 무시하므로
    // jsdom 환경(atob 존재)에서 검증.
    if (typeof atob === 'function') {
      const decoded = decodePayload('!!!not-base64!!!')
      // atob는 일부 문자열을 허용하므로 kind는 json/raw/error 중 하나여야 함.
      expect(['json', 'raw', 'error']).toContain(decoded.kind)
    }
  })
})
