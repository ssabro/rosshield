import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { FileText } from 'lucide-react'

import { apiClient } from '@/api/client'
import { ApiError, extractErrorMessage } from '@/api/errors'
import { useReports } from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { useAuthStore } from '@/stores/auth'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type { Report } from '@/api/hooks'

// `/reports` — 생성된 리포트 목록.
// - 다운로드 endpoint는 spec 미정의(Phase 2). 버튼은 disabled + 툴팁 안내.
// 컬럼: ID·session·생성일·서명 여부·SHA256(앞 16자)
function ReportsPage(): React.ReactElement {
  const reports = useReports()
  const t = useT()

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.reports.title')}
        description={t('pages.reports.description')}
      />

      <div className="rounded-md border">
        <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('reports.table.id')}</TableHead>
                <TableHead>{t('reports.table.session')}</TableHead>
                <TableHead>{t('reports.table.created')}</TableHead>
                <TableHead>{t('reports.table.signed')}</TableHead>
                <TableHead>{t('reports.table.sha256')}</TableHead>
                <TableHead className="text-right">
                  {t('reports.table.actions')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {reports.isPending && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className="text-center text-muted-foreground"
                  >
                    {t('common.loading')}
                  </TableCell>
                </TableRow>
              )}
              {reports.isError && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className="text-center text-destructive"
                  >
                    {reports.error instanceof ApiError
                      ? reports.error.message
                      : t('reports.error.fallback')}
                  </TableCell>
                </TableRow>
              )}
              {reports.isSuccess && reports.data.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="p-0">
                    <EmptyState
                      icon={FileText}
                      title={t('reports.empty.title')}
                      description={t('reports.empty.description')}
                      className="rounded-none border-0 bg-transparent"
                    />
                  </TableCell>
                </TableRow>
              )}
              {reports.isSuccess &&
                reports.data.map((report) => (
                  <ReportRow key={report.id} report={report} />
                ))}
            </TableBody>
          </Table>
      </div>
    </div>
  )
}

function ReportRow({ report }: { report: Report }): React.ReactElement {
  const t = useT()
  const sha = report.pdfSha256 ? report.pdfSha256.slice(0, 16) : '-'
  const generated = formatDate(report.generatedAt)
  return (
    <TableRow>
      <TableCell className="font-mono text-xs">{report.id}</TableCell>
      <TableCell className="font-mono text-xs">{report.sessionId}</TableCell>
      <TableCell className="text-xs">{generated}</TableCell>
      <TableCell
        aria-label={
          report.signed ? t('reports.signed.yes.aria') : t('reports.signed.no.aria')
        }
      >
        {report.signed ? (
          <span className="text-emerald-600" aria-hidden>
            ✓
          </span>
        ) : (
          <span className="text-muted-foreground" aria-hidden>
            ✗
          </span>
        )}
      </TableCell>
      <TableCell className="font-mono text-xs">{sha}</TableCell>
      <TableCell className="space-y-1 text-right">
        <div className="flex justify-end gap-1">
          <Button
            size="sm"
            variant="outline"
            onClick={() => downloadReportPDF(report.id)}
          >
            {t('reports.action.download')}
          </Button>
          {report.signed && <VerifyButton reportID={report.id} />}
        </div>
      </TableCell>
    </TableRow>
  )
}


// VerifyResult는 verify endpoint 응답 (verifyReportResponse mirror).
interface VerifyResult {
  ok: boolean
  reason?: string
  pdfSize: number
  pdfSha256: string
  signerKeyId: string
  chainHeadSeq: number
  chainHeadHash: string
}

// VerifyButton — signed report에 대해 server-side verify 실행 후 결과 inline 표시.
//
// useState로 result/loading/error 관리. ok=true는 success Badge, ok=false는 destructive Badge.
// 결과 옆 "상세" 토글 — 클릭 시 chain head + signer keyId + sha256 detail panel expand.
function VerifyButton({ reportID }: { reportID: string }): React.ReactElement {
  const t = useT()
  const [state, setState] = useState<{
    loading: boolean
    result?: VerifyResult
    error?: string
  }>({ loading: false })
  const [expanded, setExpanded] = useState(false)

  const onClick = async () => {
    setState({ loading: true })
    setExpanded(false)
    try {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/reports/{reportId}/verify',
        { params: { path: { reportId: reportID } } },
      )
      if (error) {
        setState({
          loading: false,
          error: extractErrorMessage(error, response.statusText),
        })
        return
      }
      const r = data as unknown as VerifyResult
      setState({ loading: false, result: r })
    } catch (e) {
      setState({
        loading: false,
        error: e instanceof Error ? e.message : String(e),
      })
    }
  }

  return (
    <div className="flex flex-col items-end gap-1">
      <span className="flex items-center gap-1.5">
        <Button
          size="sm"
          variant="outline"
          onClick={onClick}
          disabled={state.loading}
        >
          {state.loading ? t('reports.action.verifying') : t('reports.action.verify')}
        </Button>
        {state.result?.ok === true && (
          <Badge variant="default" className="text-[10px]">
            {t('reports.verify.ok')}
          </Badge>
        )}
        {state.result?.ok === false && (
          <Badge
            variant="destructive"
            className="text-[10px]"
            title={state.result.reason}
          >
            {t('reports.verify.failed')}
          </Badge>
        )}
        {state.result && (
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="text-[10px] text-muted-foreground underline hover:no-underline"
            aria-expanded={expanded}
          >
            {expanded ? t('reports.verify.hideDetail') : t('reports.verify.showDetail')}
          </button>
        )}
        {state.error && (
          <span className="text-xs text-destructive" title={state.error}>
            {t('reports.verify.error')}
          </span>
        )}
      </span>
      {expanded && state.result && (
        <VerifyDetail result={state.result} />
      )}
    </div>
  )
}

// VerifyDetail — verify 응답의 chain head + signer keyId + sha256 펼친 패널.
//
// 운영자가 audit anchor와 cross-check 시 활용 — 같은 chainHeadHash가 별 PC에서 외부 SDK
// 검증 결과와 일치하는지 시각 비교 가능.
function VerifyDetail({ result }: { result: VerifyResult }): React.ReactElement {
  const t = useT()
  return (
    <div className="rounded-md border border-border bg-muted/30 px-3 py-2 text-left text-[11px] font-mono">
      <DetailRow label={t('reports.verify.detail.signerKeyId')} value={result.signerKeyId || '-'} />
      <DetailRow
        label={t('reports.verify.detail.chainHeadSeq')}
        value={String(result.chainHeadSeq)}
      />
      <DetailRow
        label={t('reports.verify.detail.chainHeadHash')}
        value={result.chainHeadHash || '-'}
      />
      <DetailRow label={t('reports.verify.detail.pdfSha256')} value={result.pdfSha256 || '-'} />
      <DetailRow
        label={t('reports.verify.detail.pdfSize')}
        value={`${result.pdfSize} bytes`}
      />
      {result.reason && (
        <DetailRow label={t('reports.verify.detail.reason')} value={result.reason} />
      )}
    </div>
  )
}

function DetailRow({ label, value }: { label: string; value: string }): React.ReactElement {
  return (
    <div className="grid grid-cols-[8rem_1fr] gap-2">
      <span className="text-muted-foreground">{label}:</span>
      <span className="break-all">{value}</span>
    </div>
  )
}

function formatDate(iso: string): string {
  if (!iso) return '-'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

// downloadReportPDF — `<a download>` 트릭으로 PDF blob 다운로드.
//
// apiClient는 application/pdf binary 응답을 다루기 까다로워 raw fetch + Blob 처리.
// Authorization 헤더는 useAuthStore의 accessToken을 동기 read.
async function downloadReportPDF(reportID: string): Promise<void> {
  const token = useAuthStore.getState().accessToken
  if (!token) return
  const resp = await fetch(`/api/v1/reports/${encodeURIComponent(reportID)}/download`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!resp.ok) {
    // 실패는 console에만 — 운영자가 다시 시도. 본 PR은 download UX만이라 toast 별 epic.
    console.error('downloadReportPDF failed:', resp.status, resp.statusText)
    return
  }
  const blob = await resp.blob()
  const url = window.URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `report-${reportID}.pdf`
  document.body.appendChild(a)
  a.click()
  a.remove()
  window.URL.revokeObjectURL(url)
}

export const Route = createFileRoute('/_authenticated/reports')({
  component: ReportsPage,
})
