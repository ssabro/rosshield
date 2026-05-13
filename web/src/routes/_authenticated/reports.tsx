import { createFileRoute } from '@tanstack/react-router'

import { FileText } from 'lucide-react'

import { ApiError } from '@/api/errors'
import { useReports } from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { useAuthStore } from '@/stores/auth'
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
                  {t('reports.table.download')}
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
      <TableCell className="text-right">
        <Button
          size="sm"
          variant="outline"
          onClick={() => downloadReportPDF(report.id)}
        >
          {t('reports.action.download')}
        </Button>
      </TableCell>
    </TableRow>
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
