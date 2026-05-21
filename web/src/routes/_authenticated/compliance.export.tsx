import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { Download } from 'lucide-react'

import { useHasPermission } from '@/api/hooks'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { requirePermission } from '@/lib/route-guards'
import { toast } from '@/lib/toast'
import { mutationGuardTitle, useIsOffline } from '@/lib/use-is-offline'
import { useAuthStore } from '@/stores/auth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { DictKey } from '@/i18n/dict'

// `/compliance/export` — Phase 11.B-5 audit log export wizard.
//
// design doc `docs/design/notes/soc2-readiness-design.md` §7.5 (Stage 11.B-5).
// 외부 감사인에게 audit chain bundle (NDJSON+gzip) 을 전달하는 wizard 페이지.
//
// 권한 매트릭스 §3.3:
//   - admin: audit.read + audit.verify + audit.export
//   - auditor: audit.read + audit.verify + audit.export
//   - 그 외 role(operator/fleet-admin/read-only/owner)은 audit.export 미보유
//     → server 가 403 으로 차단 + UI 도 useHasPermission gate 로 회피.
//
// fetch 흐름:
//   1. accessToken 추출(zustand store).
//   2. POST /api/v1/compliance/export — body { fromSeq, toSeq, format }.
//   3. response.blob() → window.URL.createObjectURL → <a download> 트릭.
//   4. X-Rosshield-Audit-Entry-Seq 응답 헤더로 emit 된 audit entry seq 확인.

type BundleFormat = 'v1' | 'v2'

export function ComplianceExportPage(): React.ReactElement {
  const t = useT()
  const canExport = useHasPermission('audit', 'export')
  const isOffline = useIsOffline()
  const [fromSeq, setFromSeq] = useState('')
  const [toSeq, setToSeq] = useState('')
  const [format, setFormat] = useState<BundleFormat>('v2')
  const [submitting, setSubmitting] = useState(false)

  const submit = async (e: React.FormEvent): Promise<void> => {
    e.preventDefault()
    if (!canExport || isOffline) return
    setSubmitting(true)
    try {
      const result = await downloadAuditBundle({
        fromSeq: fromSeq.trim() ? Number(fromSeq) : 0,
        toSeq: toSeq.trim() ? Number(toSeq) : 0,
        format,
      })
      toast.success(t('compliance.export.toast.success'), {
        description: t('compliance.export.toast.success.desc', {
          seq: result.auditEntrySeq,
          format: result.format,
        }),
      })
    } catch (err) {
      toast.error(t('compliance.export.toast.error'), {
        description: exportErrorMessage(err, t),
      })
    } finally {
      setSubmitting(false)
    }
  }

  const guardTitle = mutationGuardTitle({
    isOffline,
    offlineLabel: t('pwa.offline.mutationBlocked'),
    fallback: !canExport ? t('compliance.export.error.unauthorized') : undefined,
  })

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.compliance.export.title')}
        description={t('pages.compliance.export.description')}
      />

      <form
        onSubmit={submit}
        className="space-y-4 rounded-md border p-4"
        aria-label={t('compliance.export.section')}
      >
        <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="export-from">{t('compliance.export.fromSeq')}</Label>
            <Input
              id="export-from"
              type="number"
              min={0}
              inputMode="numeric"
              placeholder={t('compliance.export.fromSeq.placeholder')}
              value={fromSeq}
              onChange={(ev) => setFromSeq(ev.target.value)}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="export-to">{t('compliance.export.toSeq')}</Label>
            <Input
              id="export-to"
              type="number"
              min={0}
              inputMode="numeric"
              placeholder={t('compliance.export.toSeq.placeholder')}
              value={toSeq}
              onChange={(ev) => setToSeq(ev.target.value)}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="export-format">{t('compliance.export.format')}</Label>
            <Select
              value={format}
              onValueChange={(v) => setFormat(v as BundleFormat)}
            >
              <SelectTrigger id="export-format">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="v2">
                  {t('compliance.export.format.v2')}
                </SelectItem>
                <SelectItem value="v1">
                  {t('compliance.export.format.v1')}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        <p className="text-xs text-muted-foreground">
          {t('compliance.export.hint')}
        </p>

        <div className="flex justify-end">
          <Button
            type="submit"
            disabled={!canExport || isOffline || submitting}
            title={guardTitle}
          >
            <Download className="mr-1.5 size-4" aria-hidden />
            {submitting
              ? t('compliance.export.submitting')
              : t('compliance.export.submit')}
          </Button>
        </div>
      </form>
    </div>
  )
}

// downloadAuditBundle — fetch + blob + <a download> 트릭으로 audit bundle 다운로드.
//
// 응답 헤더 `X-Rosshield-Audit-Entry-Seq` 와 `X-Rosshield-Export-Format` 으로 emit 된
// audit entry seq + 실 format 을 추출해 toast 에 노출합니다.
//
// 단위 test 대상 (compliance.export.test.tsx 가 useState/blob/fetch mock).
export interface ExportRequest {
  fromSeq: number
  toSeq: number
  format: BundleFormat
}

export interface ExportResult {
  auditEntrySeq: string
  format: string
}

export async function downloadAuditBundle(
  req: ExportRequest,
): Promise<ExportResult> {
  const token = useAuthStore.getState().accessToken
  if (!token) {
    throw new ExportApiError(401, 'no access token')
  }
  const resp = await fetch('/api/v1/compliance/export', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      fromSeq: req.fromSeq,
      toSeq: req.toSeq,
      format: req.format,
    }),
  })
  if (!resp.ok) {
    throw new ExportApiError(resp.status, await safeText(resp))
  }
  const blob = await resp.blob()
  const filename = filenameFromResponse(resp)
  triggerDownload(blob, filename)
  return {
    auditEntrySeq: resp.headers.get('X-Rosshield-Audit-Entry-Seq') ?? '',
    format: resp.headers.get('X-Rosshield-Export-Format') ?? req.format,
  }
}

// ExportApiError — 다운로드 실패 시 status + 본문 message 보존.
export class ExportApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
    this.name = 'ExportApiError'
  }
}

async function safeText(resp: Response): Promise<string> {
  try {
    return await resp.text()
  } catch {
    return resp.statusText
  }
}

function filenameFromResponse(resp: Response): string {
  const cd = resp.headers.get('Content-Disposition') ?? ''
  const m = cd.match(/filename="([^"]+)"/)
  return m?.[1] ?? `audit-bundle-${Date.now()}.ndjson.gz`
}

function triggerDownload(blob: Blob, filename: string): void {
  const url = window.URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  window.URL.revokeObjectURL(url)
}

// exportErrorMessage — server 에러를 친화 메시지로 매핑.
// 단위 test 대상 (compliance.export.test.tsx).
export function exportErrorMessage(
  err: unknown,
  t: (key: DictKey, vars?: Record<string, string | number>) => string,
): string {
  if (err instanceof ExportApiError) {
    if (err.status === 401 || err.status === 403) {
      return t('compliance.export.error.unauthorized')
    }
    if (err.status === 503) {
      return t('compliance.export.error.unavailable')
    }
    return err.message || t('compliance.export.error.fallback')
  }
  return t('compliance.export.error.fallback')
}

export const Route = createFileRoute('/_authenticated/compliance/export')({
  beforeLoad: () => requirePermission('audit', 'export'),
  component: ComplianceExportPage,
})
