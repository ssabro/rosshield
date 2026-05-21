// Phase 11.B-5 — audit log export wizard helper 단위 테스트.
//
// page 마운트 대신 export 된 helper(`exportErrorMessage`, `downloadAuditBundle`,
// `ExportApiError`) 직접 검증. fetch + URL.createObjectURL 등 browser API 는 mock.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  downloadAuditBundle,
  exportErrorMessage,
  ExportApiError,
} from './compliance.export'
import { useAuthStore } from '@/stores/auth'

import type { DictKey } from '@/i18n/dict'

// translation stub — key 를 그대로 반환해 매핑 분기 검증.
function tStub(key: DictKey, vars?: Record<string, string | number>): string {
  if (!vars) return key
  const pairs = Object.entries(vars)
    .map(([k, v]) => `${k}=${v}`)
    .join(' ')
  return `${key} [${pairs}]`
}

describe('exportErrorMessage', () => {
  it('401 → unauthorized 메시지', () => {
    const msg = exportErrorMessage(new ExportApiError(401, 'no token'), tStub)
    expect(msg).toBe('compliance.export.error.unauthorized')
  })

  it('403 → unauthorized 메시지', () => {
    const msg = exportErrorMessage(
      new ExportApiError(403, 'forbidden'),
      tStub,
    )
    expect(msg).toBe('compliance.export.error.unauthorized')
  })

  it('503 → unavailable 메시지', () => {
    const msg = exportErrorMessage(
      new ExportApiError(503, 'audit log export not configured'),
      tStub,
    )
    expect(msg).toBe('compliance.export.error.unavailable')
  })

  it('500 + body 있음 → body message', () => {
    const msg = exportErrorMessage(
      new ExportApiError(500, 'internal err'),
      tStub,
    )
    expect(msg).toBe('internal err')
  })

  it('non-ApiError → fallback', () => {
    const msg = exportErrorMessage(new Error('network'), tStub)
    expect(msg).toBe('compliance.export.error.fallback')
  })

  it('unknown(null) → fallback', () => {
    const msg = exportErrorMessage(null, tStub)
    expect(msg).toBe('compliance.export.error.fallback')
  })
})

describe('downloadAuditBundle', () => {
  let originalFetch: typeof globalThis.fetch
  let originalCreateUrl: typeof window.URL.createObjectURL
  let originalRevokeUrl: typeof window.URL.revokeObjectURL

  beforeEach(() => {
    originalFetch = globalThis.fetch
    originalCreateUrl = window.URL.createObjectURL
    originalRevokeUrl = window.URL.revokeObjectURL
    window.URL.createObjectURL = vi.fn(() => 'blob:mock')
    window.URL.revokeObjectURL = vi.fn()
    useAuthStore.setState({ accessToken: 'tk_test' })
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
    window.URL.createObjectURL = originalCreateUrl
    window.URL.revokeObjectURL = originalRevokeUrl
    useAuthStore.setState({ accessToken: null })
  })

  it('accessToken 부재 → 401 ExportApiError', async () => {
    useAuthStore.setState({ accessToken: null })
    await expect(
      downloadAuditBundle({ fromSeq: 0, toSeq: 0, format: 'v2' }),
    ).rejects.toThrow(ExportApiError)
  })

  it('서버 503 → ExportApiError(status=503)', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      statusText: 'Service Unavailable',
      text: async () => 'audit log export not configured',
    } as Response)
    let err: unknown
    try {
      await downloadAuditBundle({ fromSeq: 0, toSeq: 0, format: 'v2' })
    } catch (e) {
      err = e
    }
    expect(err).toBeInstanceOf(ExportApiError)
    expect((err as ExportApiError).status).toBe(503)
  })

  it('happy path → result.auditEntrySeq + format 반환', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: {
        get: (k: string) => {
          if (k === 'X-Rosshield-Audit-Entry-Seq') return '42'
          if (k === 'X-Rosshield-Export-Format') return 'v2'
          if (k === 'Content-Disposition')
            return `attachment; filename="audit-bundle-system-X.ndjson.gz"`
          return null
        },
      },
      blob: async () => new Blob(['mock-gzip-content'], { type: 'application/gzip' }),
    } as unknown as Response)

    const result = await downloadAuditBundle({
      fromSeq: 1,
      toSeq: 3,
      format: 'v2',
    })
    expect(result.auditEntrySeq).toBe('42')
    expect(result.format).toBe('v2')
    expect(window.URL.createObjectURL).toHaveBeenCalled()
    expect(window.URL.revokeObjectURL).toHaveBeenCalled()
  })
})

describe('ExportApiError', () => {
  it('status + message 보존', () => {
    const err = new ExportApiError(403, 'forbidden')
    expect(err.status).toBe(403)
    expect(err.message).toBe('forbidden')
    expect(err.name).toBe('ExportApiError')
    expect(err).toBeInstanceOf(Error)
  })
})
