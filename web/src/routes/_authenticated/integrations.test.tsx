// B3 — `/integrations` 페이지 helper 단위 테스트.
//
// 페이지 마운트 자체는 TanStack Router 의존이라 회피 — 다른 페이지(advisor, compliance)
// 와 동일한 패턴으로 helper export 함수만 검증.

import { describe, expect, it } from 'vitest'

import {
  formatWebhookEvent,
  webhookDeliveryStatus,
  type WebhookEndpoint,
} from '@/api/hooks'

import {
  deliveryStatusLabelKey,
  endpointDisplayName,
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
