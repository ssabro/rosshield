import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { ChevronDown, ChevronRight } from 'lucide-react'
import * as React from 'react'
import { useCallback, useEffect, useState } from 'react'

import { apiClient } from '@/api/client'
import { extractErrorMessage } from '@/api/errors'
import {
  useDeleteRobot,
  useFleet,
  useHasPermission,
  usePacks,
  useRobot,
  useRobotResults,
  useRotateCredential,
} from '@/api/hooks'
import { Breadcrumbs } from '@/components/layout/Breadcrumbs'
import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { CardSkeleton } from '@/components/ui/skeleton'
import { useT } from '@/i18n/t'
import { confirm } from '@/lib/confirm'
import { toast } from '@/lib/toast'
import { undoableAction } from '@/lib/undoable'

import type { FormEvent } from 'react'

import type { Robot, RobotResult } from '@/api/hooks'

// `/robots/$robotId` — 단일 robot 상세 (모든 인증 사용자).
function RobotDetailPage(): React.ReactElement {
  const { robotId } = Route.useParams()
  return <RobotDetailView robotId={robotId} />
}

// a11y-drilldown.test.tsx mount용 named export — Route.useParams 의존 분리.
export function RobotDetailView({ robotId }: { robotId: string }): React.ReactElement {
  const t = useT()
  const robotQuery = useRobot(robotId)
  const robot = robotQuery.data
  // fleetId는 robot fetch 후에만 알 수 있음 — useFleet은 enabled !!fleetId.
  const fleetQuery = useFleet(robot?.fleetId)

  if (robotQuery.isPending) {
    return (
      <div className="space-y-6">
        <PageHeader title={t('pages.robots.title')} />
        <CardSkeleton />
        <p className="sr-only">{t('robots.detail.loading')}</p>
      </div>
    )
  }
  if (!robot || robotQuery.isError) {
    return (
      <div className="space-y-6">
        <PageHeader title={t('pages.robots.title')} />
        <Card>
          <CardContent className="py-6 text-sm text-destructive">
            {t('robots.detail.notFound')}{' '}
            <Link to="/robots" className="underline">
              {t('robots.detail.back')}
            </Link>
          </CardContent>
        </Card>
      </div>
    )
  }

  const tags = Array.isArray(robot.tags) ? (robot.tags as unknown[]) : []

  const role = typeof robot.role === 'string' ? robot.role : ''
  const osDistro = typeof robot.osDistro === 'string' ? robot.osDistro : ''
  const rosDistro = typeof robot.rosDistro === 'string' ? robot.rosDistro : ''

  // D-UI-1 Stage 4 — PageHeader subtitle을 robot meta(host · auth · criticality)로
  //   강화. role이 있으면 우선 노출. Breadcrumbs는 PageHeader slot 사용.
  const subtitleParts: string[] = []
  if (role) subtitleParts.push(role)
  subtitleParts.push(`${robot.host}:${robot.port}`)
  subtitleParts.push(robot.authType)
  const subtitle = subtitleParts.join(' · ')

  return (
    <div className="space-y-6">
      <PageHeader
        title={robot.name}
        description={subtitle}
        breadcrumbs={
          <Breadcrumbs
            items={[
              { label: t('nav.fleets'), to: '/fleets' },
              {
                label: fleetQuery.data?.name ?? robot.fleetId,
                to: '/fleets/$fleetId',
                params: { fleetId: robot.fleetId },
              },
              { label: t('nav.robots'), to: '/robots' },
              { label: robot.name },
            ]}
          />
        }
        badge={<Badge variant="secondary">{robot.criticality}</Badge>}
      />

      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle>{t('robots.detail.metaTitle')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <MetaRow label={t('robots.detail.id')} value={<span className="font-mono">{robot.id}</span>} />
          <MetaRow
            label={t('robots.detail.fleet')}
            value={
              <Link
                to="/fleets/$fleetId"
                params={{ fleetId: robot.fleetId }}
                className="font-mono hover:underline"
              >
                {fleetQuery.data?.name ?? robot.fleetId}
              </Link>
            }
          />
          <MetaRow
            label={t('robots.detail.host')}
            value={
              <span className="font-mono">
                {robot.host}:{robot.port}
              </span>
            }
          />
          <MetaRow label={t('robots.detail.authType')} value={robot.authType} />
          <MetaRow
            label={t('robots.detail.criticality')}
            value={<Badge variant="secondary">{robot.criticality}</Badge>}
          />
          {osDistro && (
            <MetaRow
              label={t('robots.detail.osDistro')}
              value={<span className="font-mono">{osDistro}</span>}
            />
          )}
          {rosDistro && (
            <MetaRow
              label={t('robots.detail.rosDistro')}
              value={<span className="font-mono">{rosDistro}</span>}
            />
          )}
          <MetaRow
            label={t('robots.detail.tags')}
            value={
              tags.length === 0 ? (
                <span className="text-xs text-muted-foreground">-</span>
              ) : (
                <div className="flex flex-wrap gap-1">
                  {tags.map((tag, i) => (
                    <Badge key={i} variant="outline" className="text-xs">
                      {String(tag)}
                    </Badge>
                  ))}
                </div>
              )
            }
          />
        </CardContent>
      </Card>

      <RobotResultsCard robotId={robot.id} />

      <RotateCredentialCard robotId={robot.id} fleetId={robot.fleetId} />

      <DeleteRobotCard robot={robot} />

      <p className="text-xs text-muted-foreground">
        <Link to="/robots" className="underline">
          {t('robots.detail.back')}
        </Link>
      </p>
    </div>
  )
}

// DeleteRobotCard — admin/fleet-admin only. typing confirmation (robot name) +
// toast 성공 통지. 성공 시 /robots로 navigate.
//
// RBAC Stage 5 — server `RequirePermission(robot, write)` 매핑 (§2.2 ID 5).
// fleet 컨텍스트는 robot.fleetId. admin tenant scope는 fleetId 무관 통과 (회귀 0).
//
// D-UI-1 Stage 4 — window.confirm/inline 2-step 대신 imperative confirm() Promise
// + typing confirmation (robot.name 입력 필요)으로 실수 차단 강도↑.
function DeleteRobotCard({ robot }: { robot: Robot }): React.ReactElement | null {
  const t = useT()
  const canDelete = useHasPermission('robot', 'write', robot.fleetId)
  const navigate = useNavigate()
  const del = useDeleteRobot()

  if (!canDelete) return null

  const handleDeleteClick = async (): Promise<void> => {
    const ok = await confirm({
      title: t('robots.detail.delete.confirmTitle'),
      description: `${t('robots.detail.delete.confirmBody')}\n\n${t('robots.detail.delete.confirm.typingHint')}`,
      confirmText: robot.name,
      confirmLabel: t('robots.detail.delete.confirm.button'),
      cancelLabel: t('robots.detail.delete.cancel'),
      destructive: true,
    })
    if (!ok) return
    // D-UI-1 P0 — Undo window: ConfirmDialog 후 5초 보류. 사용자가 undo를 누를
    //   가능성 있어 navigate는 즉시가 아닌 mutation 후 onSuccess에서 수행.
    undoableAction({
      message: t('robots.detail.delete.toast.success'),
      undoLabel: t('common.undo'),
      action: async () => {
        await del.mutateAsync(robot.id)
        void navigate({ to: '/robots', replace: true })
      },
      errorLabel: t('robots.detail.delete.error'),
    })
  }

  return (
    <Card className="max-w-xl">
      <CardHeader>
        <CardTitle className="text-sm">{t('robots.detail.delete.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        <Button
          variant="destructive"
          size="sm"
          disabled={del.isPending}
          onClick={() => {
            void handleDeleteClick()
          }}
        >
          {del.isPending
            ? t('robots.detail.delete.pending')
            : t('robots.detail.delete.button')}
        </Button>
      </CardContent>
    </Card>
  )
}

function MetaRow({
  label,
  value,
}: {
  label: string
  value: React.ReactNode
}): React.ReactElement {
  return (
    <div className="flex items-baseline gap-2">
      <span className="w-32 shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 flex-1">{value}</span>
    </div>
  )
}

// RobotResultsCard — useRobotResults hook으로 최근 진단 결과 20개를 session 단위 그룹으로 표시.
//
// packKey는 서버 응답에서 직접 옴 (RobotResult.packKey, scan_sessions→packs JOIN 결과).
// packIsBuiltin은 usePacks(이미 cache 공유)로 client-side 매핑.
// collapsed 상태는 localStorage에 sessionId Set으로 보존 — 새로고침 후 같은 그룹은 접힌 채.
function RobotResultsCard({ robotId }: { robotId: string }): React.ReactElement {
  const t = useT()
  const q = useRobotResults(robotId, 20)
  const packsQuery = usePacks()
  const results = q.data ?? []
  const builtinByPackKey = new Map<string, boolean>()
  for (const p of packsQuery.data ?? []) {
    builtinByPackKey.set(p.packKey, p.isBuiltin)
  }
  const { isCollapsed, toggle, setMany } = useCollapsedSessions()
  const groups = groupBySession(results)
  const groupIds = groups.map((g) => g.sessionId)
  const allCollapsed = groupIds.length > 0 && groupIds.every((id) => isCollapsed(id))

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>{t('robots.detail.results.title')}</CardTitle>
        {groups.length > 1 && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => setMany(groupIds, !allCollapsed)}
            className="h-7 text-xs"
          >
            {allCollapsed
              ? t('robots.detail.results.expandAll')
              : t('robots.detail.results.collapseAll')}
          </Button>
        )}
      </CardHeader>
      <CardContent>
        {q.isPending ? (
          <p className="text-sm text-muted-foreground">
            {t('robots.detail.results.loading')}
          </p>
        ) : q.isError ? (
          <p className="text-sm text-destructive">
            {q.error instanceof Error
              ? q.error.message
              : t('robots.detail.results.error')}
          </p>
        ) : results.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {t('robots.detail.results.empty')}
          </p>
        ) : (
          <div className="space-y-3">
            {groups.map((group) => (
              <SessionGroup
                key={group.sessionId}
                group={group}
                builtinByPackKey={builtinByPackKey}
                collapsed={isCollapsed(group.sessionId)}
                onToggle={() => toggle(group.sessionId)}
              />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

// useCollapsedSessions — RobotResultsCard SessionGroup 접힘 상태 localStorage 보존.
//
// 키: rosshield.ui.robotResults.collapsedSessions — JSON Array<sessionId>.
// 기본 펼침(Set 비-멤버), toggle/setMany는 즉시 localStorage 동기화.
const collapsedStorageKey = 'rosshield.ui.robotResults.collapsedSessions'

function persistCollapsed(set: Set<string>): void {
  try {
    window.localStorage.setItem(
      collapsedStorageKey,
      JSON.stringify(Array.from(set)),
    )
  } catch {
    // localStorage quota 초과 또는 비활성 — silent.
  }
}

function useCollapsedSessions(): {
  isCollapsed: (id: string) => boolean
  toggle: (id: string) => void
  setMany: (ids: string[], collapsed: boolean) => void
} {
  const [collapsed, setCollapsed] = useState<Set<string>>(() => {
    if (typeof window === 'undefined') return new Set()
    try {
      const raw = window.localStorage.getItem(collapsedStorageKey)
      if (!raw) return new Set()
      const arr = JSON.parse(raw) as unknown
      if (!Array.isArray(arr)) return new Set()
      return new Set(arr.filter((v): v is string => typeof v === 'string'))
    } catch {
      return new Set()
    }
  })
  const isCollapsed = useCallback((id: string) => collapsed.has(id), [collapsed])
  const toggle = useCallback((id: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      persistCollapsed(next)
      return next
    })
  }, [])
  const setMany = useCallback((ids: string[], shouldCollapse: boolean) => {
    setCollapsed((prev) => {
      const next = new Set(prev)
      for (const id of ids) {
        if (shouldCollapse) {
          next.add(id)
        } else {
          next.delete(id)
        }
      }
      persistCollapsed(next)
      return next
    })
  }, [])
  return { isCollapsed, toggle, setMany }
}

interface SessionResultGroup {
  sessionId: string
  results: RobotResult[]
}

// groupBySession는 결과 배열을 session 단위로 그룹 (서버 정렬 executed_at DESC 보존).
//
// 같은 session 내 결과들은 도메인 정렬을 그대로 따른다 — sort 안 함(서버가 결정 의도).
function groupBySession(results: RobotResult[]): SessionResultGroup[] {
  const groups: SessionResultGroup[] = []
  const idx = new Map<string, number>()
  for (const r of results) {
    const i = idx.get(r.sessionId)
    if (i === undefined) {
      idx.set(r.sessionId, groups.length)
      groups.push({ sessionId: r.sessionId, results: [r] })
    } else {
      groups[i].results.push(r)
    }
  }
  return groups
}

function SessionGroup({
  group,
  builtinByPackKey,
  collapsed,
  onToggle,
}: {
  group: SessionResultGroup
  builtinByPackKey: Map<string, boolean>
  collapsed: boolean
  onToggle: () => void
}): React.ReactElement {
  const t = useT()
  const ChevronIcon = collapsed ? ChevronRight : ChevronDown
  // sessionStartedAt/CompletedAt/FailureReason/Status는 같은 그룹 내 모든 result에 동일 — 첫 result에서 추출.
  const first = group.results[0]
  const startedAt = first?.sessionStartedAt
  const completedAt = first?.sessionCompletedAt
  const failureReason = first?.sessionFailureReason
  const status = first?.sessionStatus
  const totalDuration = formatTotalDuration(startedAt, completedAt)
  return (
    <div className="space-y-1">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={!collapsed}
          aria-label={
            collapsed
              ? t('robots.detail.results.expand')
              : t('robots.detail.results.collapse')
          }
          className="flex items-center gap-1 rounded p-0.5 hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          <ChevronIcon className="h-3 w-3" />
          <span>{t('robots.detail.results.session')}:</span>
        </button>
        <Link
          to="/scans"
          search={{ session: group.sessionId }}
          className="font-mono hover:text-foreground hover:underline"
        >
          {group.sessionId}
        </Link>
        {status && (
          <Badge variant={sessionStatusVariant(status)} className="text-[10px]">
            {status}
          </Badge>
        )}
        {startedAt && (
          <span
            title={new Date(startedAt).toLocaleString()}
            className="text-muted-foreground"
          >
            · {t('robots.detail.results.startedAt')} {formatRelative(startedAt)}
          </span>
        )}
        {totalDuration && (
          <span
            title={
              completedAt ? new Date(completedAt).toLocaleString() : undefined
            }
            className="text-muted-foreground"
          >
            · {t('robots.detail.results.totalDuration')} {totalDuration}
          </span>
        )}
        <span className="ml-auto">
          {t('robots.detail.results.count', { count: group.results.length })}
        </span>
      </div>
      {failureReason && status === 'failed' && (
        <FailureReasonAlert reason={failureReason} />
      )}
      {!collapsed && (
        <div className="space-y-1">
          {group.results.map((r) => (
            <ResultRow
              key={r.id}
              result={r}
              builtinByPackKey={builtinByPackKey}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function ResultRow({
  result,
  builtinByPackKey,
}: {
  result: RobotResult
  builtinByPackKey: Map<string, boolean>
}): React.ReactElement {
  const t = useT()
  const packKey = result.packKey
  // isBuiltin은 packKey가 있고 packs cache에 있을 때만 결정. 미해결이면 Badge 숨김.
  const isBuiltin = packKey ? builtinByPackKey.get(packKey) : undefined
  return (
    <div className="flex items-center justify-between rounded border border-border px-3 py-2 text-sm">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <Badge variant={outcomeVariant(result.outcome)}>{result.outcome}</Badge>
          {packKey ? (
            <Link
              to="/packs/$packKey/checks/$checkId"
              params={{ packKey, checkId: result.checkId }}
              className="font-mono text-xs hover:underline"
            >
              {result.checkId}
            </Link>
          ) : (
            <span className="font-mono text-xs">{result.checkId}</span>
          )}
          {isBuiltin !== undefined && (
            <Badge
              variant={isBuiltin ? 'secondary' : 'outline'}
              className="text-[10px]"
            >
              {isBuiltin ? t('packs.scope.builtin') : t('packs.scope.tenant')}
            </Badge>
          )}
        </div>
        {result.evalReason && (
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            {result.evalReason}
          </p>
        )}
      </div>
      <div className="ml-4 shrink-0 text-xs text-muted-foreground">
        <div className="text-right">{formatRelative(result.executedAt)}</div>
        <div className="text-right">{result.durationMs}ms</div>
      </div>
    </div>
  )
}

function outcomeVariant(
  outcome: string,
): 'default' | 'destructive' | 'secondary' | 'outline' {
  switch (outcome) {
    case 'pass':
      return 'default'
    case 'fail':
    case 'error':
      return 'destructive'
    case 'indeterminate':
      return 'secondary'
    default:
      return 'outline'
  }
}

// sessionStatusVariant — pending/running/completed/failed/cancelled를 Badge variant로 매핑.
// completed=default(녹색 강조)·failed=destructive·running=secondary·cancelled=warning(노랑)·pending=outline.
function sessionStatusVariant(
  status: string,
): 'default' | 'destructive' | 'secondary' | 'outline' | 'warning' {
  switch (status) {
    case 'completed':
      return 'default'
    case 'failed':
      return 'destructive'
    case 'running':
      return 'secondary'
    case 'cancelled':
      return 'warning'
    case 'pending':
    default:
      return 'outline'
  }
}

// FAILURE_REASON_TRUNCATE_THRESHOLD — 한 줄 평균 길이 + 줄바꿈 개수 기반 임계값.
// 이 이하면 truncate 없이 그대로, 초과 또는 줄바꿈 2개+면 expand 토글 노출.
const FAILURE_REASON_TRUNCATE_THRESHOLD = 120

// FailureReasonAlert — destructive 톤 alert + 임계값 초과 시 'Show more' 토글.
//
// 짧은 단일 줄(SSH 연결 실패 등)은 그대로 노출. 긴 stack trace 또는 여러 줄 메시지는
// 첫 줄(또는 첫 120자)만 보이고 클릭으로 expand/collapse. monospace pre-wrap으로
// 들여쓰기·줄바꿈 보존 (audit·디버깅 가독성).
function FailureReasonAlert({ reason }: { reason: string }): React.ReactElement {
  const t = useT()
  const [expanded, setExpanded] = React.useState(false)
  const lineCount = reason.split('\n').length
  const isLong =
    reason.length > FAILURE_REASON_TRUNCATE_THRESHOLD || lineCount > 1
  const displayed = expanded || !isLong ? reason : truncateForFailureReason(reason)
  return (
    <div className="rounded border border-destructive/30 bg-destructive/5 px-2 py-1 text-xs text-destructive">
      <div className="font-medium">
        {t('robots.detail.results.failureReason')}:
      </div>
      {expanded ? (
        <pre className="mt-1 whitespace-pre-wrap break-words font-mono text-[11px]">
          {displayed}
        </pre>
      ) : (
        <span className="break-words">{displayed}</span>
      )}
      {isLong && (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="ml-2 text-[11px] underline hover:no-underline"
        >
          {expanded
            ? t('robots.detail.results.showLess')
            : t('robots.detail.results.showMore')}
        </button>
      )}
    </div>
  )
}

// truncateForFailureReason — 첫 줄 또는 첫 N자(임계 - 20)에서 자르고 ellipsis.
function truncateForFailureReason(reason: string): string {
  const firstLine = reason.split('\n')[0] ?? ''
  const limit = FAILURE_REASON_TRUNCATE_THRESHOLD - 20
  if (firstLine.length <= limit) {
    return firstLine + (reason.includes('\n') ? ' …' : '')
  }
  return firstLine.slice(0, limit) + '…'
}

function formatRelative(iso?: string): string {
  if (!iso) return ''
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  const sec = Math.round((Date.now() - t) / 1000)
  if (sec < 60) return `${sec}s`
  const min = Math.round(sec / 60)
  if (min < 60) return `${min}m`
  const hr = Math.round(min / 60)
  if (hr < 24) return `${hr}h`
  const day = Math.round(hr / 24)
  return `${day}d`
}

// formatTotalDuration는 두 ISO timestamp 사이의 절대 duration을 압축 표기로 반환합니다.
// 둘 중 하나가 없으면 빈 string. 음수/invalid도 빈 string. 60s 미만 "Ns",
// 3600s 미만 "Nm Ns" (초 0이면 생략), 그 이상 "Nh Nm".
function formatTotalDuration(start?: string, end?: string): string {
  if (!start || !end) return ''
  const a = Date.parse(start)
  const b = Date.parse(end)
  if (Number.isNaN(a) || Number.isNaN(b)) return ''
  const sec = Math.round((b - a) / 1000)
  if (sec < 0) return ''
  if (sec < 60) return `${sec}s`
  const min = Math.floor(sec / 60)
  const remSec = sec % 60
  if (min < 60) {
    return remSec > 0 ? `${min}m ${remSec}s` : `${min}m`
  }
  const hr = Math.floor(min / 60)
  const remMin = min % 60
  return remMin > 0 ? `${hr}h ${remMin}m` : `${hr}h`
}

// RotateCredentialCard — admin/fleet-admin only. 평문 자격증명 입력 → 도메인 KEK 재wrap.
//
// RBAC Stage 5 — server `RequirePermission(robot, admin)` 매핑 (§2.2 ID 6 — sensitive
// rotate 권한). fleet 컨텍스트는 robot.fleetId. admin/owner tenant scope는 fleetId
// 무관 통과 — fleet-admin은 fleet 일치 시만 (operator는 robot.admin 미보유).
function RotateCredentialCard({
  robotId,
  fleetId,
}: {
  robotId: string
  fleetId: string
}): React.ReactElement | null {
  const t = useT()
  const canRotate = useHasPermission('robot', 'admin', fleetId)
  const rotate = useRotateCredential()
  const [open, setOpen] = useState(false)
  const [authType, setAuthType] = useState<'password' | 'privateKey'>('password')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [privateKeyPem, setPrivateKeyPem] = useState('')
  const [privateKeyPassphrase, setPrivateKeyPassphrase] = useState('')
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')

  if (!canRotate) return null

  const reset = () => {
    setAuthType('password')
    setUsername('')
    setPassword('')
    setPrivateKeyPem('')
    setPrivateKeyPassphrase('')
    setError('')
  }

  // D-UI-1 Stage 4 — rotate는 irreversible + 운영 risk 큰 작업.
  //   submit 전에 confirm() typing confirmation으로 한 번 더 차단. 성공 시 toast.
  const handleSubmit = async (e: FormEvent<HTMLFormElement>): Promise<void> => {
    e.preventDefault()
    setError('')
    setSuccess('')
    const ok = await confirm({
      title: t('robots.detail.rotate.confirm.title'),
      description: t('robots.detail.rotate.confirm.description'),
      confirmText: 'rotate',
      confirmLabel: t('robots.detail.rotate.confirm.button'),
      cancelLabel: t('robots.detail.rotate.cancel'),
      destructive: true,
    })
    if (!ok) return
    rotate.mutate(
      {
        robotId,
        authType,
        username,
        password: authType === 'password' ? password : undefined,
        privateKeyPem: authType === 'privateKey' ? privateKeyPem : undefined,
        privateKeyPassphrase:
          authType === 'privateKey' && privateKeyPassphrase
            ? privateKeyPassphrase
            : undefined,
      },
      {
        onSuccess: (data) => {
          toast.success(t('robots.detail.rotate.toast.success'), {
            description: t('robots.detail.rotate.toast.successDescription', {
              id: data.newCredentialId,
            }),
          })
          setSuccess(
            t('robots.detail.rotate.success', { id: data.newCredentialId }),
          )
          reset()
          setOpen(false)
        },
        onError: (e) => {
          const msg = e instanceof Error ? e.message : t('robots.detail.rotate.error')
          setError(msg)
          toast.error(msg)
        },
      },
    )
  }

  if (!open) {
    return (
      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle className="text-sm">{t('robots.detail.rotate.title')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
          <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
            {t('robots.detail.rotate.button')}
          </Button>
          {success && <p className="text-xs text-foreground">{success}</p>}
        </CardContent>
      </Card>
    )
  }

  return (
    <Card className="max-w-xl">
      <CardHeader>
        <CardTitle className="text-sm">{t('robots.detail.rotate.formTitle')}</CardTitle>
      </CardHeader>
      <CardContent>
        <form
          onSubmit={(e) => {
            void handleSubmit(e)
          }}
          className="space-y-3 text-sm"
        >
          <div className="space-y-2">
            <Label htmlFor="rot-authtype">{t('robots.detail.rotate.authType')}</Label>
            <Select
              value={authType}
              onValueChange={(v) => setAuthType(v as 'password' | 'privateKey')}
            >
              <SelectTrigger id="rot-authtype">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="password">password</SelectItem>
                <SelectItem value="privateKey">privateKey</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="rot-user">{t('robots.detail.rotate.username')}</Label>
            <Input
              id="rot-user"
              required
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="off"
            />
          </div>
          {authType === 'password' ? (
            <div className="space-y-2">
              <Label htmlFor="rot-pw">{t('robots.detail.rotate.password')}</Label>
              <Input
                id="rot-pw"
                type="password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="new-password"
              />
            </div>
          ) : (
            <>
              <div className="space-y-2">
                <Label htmlFor="rot-pem">{t('robots.detail.rotate.privateKey')}</Label>
                <textarea
                  id="rot-pem"
                  required
                  value={privateKeyPem}
                  onChange={(e) => setPrivateKeyPem(e.target.value)}
                  className="min-h-[120px] w-full rounded border border-input bg-background px-3 py-2 font-mono text-xs"
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----..."
                  autoComplete="off"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="rot-pass">
                  {t('robots.detail.rotate.privateKeyPassphrase')}
                </Label>
                <Input
                  id="rot-pass"
                  type="password"
                  value={privateKeyPassphrase}
                  onChange={(e) => setPrivateKeyPassphrase(e.target.value)}
                  autoComplete="off"
                  placeholder={t('robots.detail.rotate.optional')}
                />
              </div>
              <PemFingerprintPreview
                pem={privateKeyPem}
                passphrase={privateKeyPassphrase}
              />
              <PubKeyFingerprintCompare />
            </>
          )}
          {error && (
            <p className="text-xs text-destructive" role="alert">
              {error}
            </p>
          )}
          <div className="flex items-center gap-2">
            <Button type="submit" size="sm" disabled={rotate.isPending}>
              {rotate.isPending
                ? t('robots.detail.rotate.pending')
                : t('robots.detail.rotate.submit')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                setOpen(false)
                reset()
              }}
            >
              {t('robots.detail.rotate.cancel')}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}

// PemFingerprintPreview — POST /api/v1/utils/ssh-fingerprint 결과로 표준 OpenSSH SHA256
// fingerprint와 keyType 표시 (공개키 기반, ssh.FingerprintSHA256). 빈 PEM이면 hidden.
//
// debounce 400ms로 한 글자마다 호출 회피. 암호화된 키 + passphrase 누락은 backend가
// 명확한 메시지로 400 → "passphrase required" 표시 (사용자 입력 유도).
function PemFingerprintPreview({
  pem,
  passphrase,
}: {
  pem: string
  passphrase: string
}): React.ReactElement | null {
  const t = useT()
  const [result, setResult] = useState<{
    fingerprint: string
    keyType: string
  } | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    const trimmed = pem.trim()
    if (!trimmed) {
      setResult(null)
      setError('')
      return
    }
    let cancelled = false
    const handle = window.setTimeout(() => {
      void (async () => {
        try {
          const { data, error: apiError, response } = await apiClient.POST(
            '/api/v1/utils/ssh-fingerprint',
            { body: { privateKeyPem: trimmed, passphrase: passphrase || undefined } },
          )
          if (cancelled) return
          if (apiError) {
            setResult(null)
            setError(extractErrorMessage(apiError, response.statusText))
            return
          }
          setResult({
            fingerprint: data.fingerprint,
            keyType: data.keyType,
          })
          setError('')
        } catch (e) {
          if (cancelled) return
          setResult(null)
          setError(e instanceof Error ? e.message : String(e))
        }
      })()
    }, 400)
    return () => {
      cancelled = true
      window.clearTimeout(handle)
    }
  }, [pem, passphrase])

  if (error) {
    return (
      <p
        className="font-mono text-xs text-destructive"
        role="status"
        aria-live="polite"
      >
        {t('robots.detail.rotate.fingerprintError')}: {error}
      </p>
    )
  }
  if (!result) return null
  return (
    <p className="font-mono text-xs text-muted-foreground" aria-live="polite">
      {t('robots.detail.rotate.fingerprint')}: {result.fingerprint}{' '}
      <span className="text-[10px]">({result.keyType})</span>
    </p>
  )
}

// PubKeyFingerprintCompare — collapsed expandable. 운영자가 자신이 가진 .pub을 붙여넣어
// fingerprint를 사전 확인 (PEM textarea의 fingerprint와 일치 여부 운영자가 시각 비교).
//
// client-side SubtleCrypto로 fingerprint 계산 — 별 endpoint 호출 없이 즉시. OpenSSH .pub
// format: `ssh-<algo> <base64-data> <comment>`. fingerprint = SHA-256(base64-decoded-data)
// → base64 (no pad).
function PubKeyFingerprintCompare(): React.ReactElement {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [pubKey, setPubKey] = useState('')
  const [computed, setComputed] = useState<{
    fingerprint: string
    keyType: string
  } | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    const trimmed = pubKey.trim()
    if (!trimmed) {
      setComputed(null)
      setError('')
      return
    }
    let cancelled = false
    void (async () => {
      try {
        const result = await computePubKeyFingerprint(trimmed)
        if (cancelled) return
        setComputed(result)
        setError('')
      } catch (e) {
        if (cancelled) return
        setComputed(null)
        setError(e instanceof Error ? e.message : String(e))
      }
    })()
    return () => {
      cancelled = true
    }
  }, [pubKey])

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="text-[11px] text-muted-foreground underline hover:no-underline"
      >
        {t('robots.detail.rotate.pubCompareToggle')}
      </button>
    )
  }
  return (
    <div className="space-y-2 rounded border border-input bg-muted/20 px-3 py-2">
      <Label htmlFor="rot-pub" className="text-xs">
        {t('robots.detail.rotate.pubCompareLabel')}
      </Label>
      <textarea
        id="rot-pub"
        value={pubKey}
        onChange={(e) => setPubKey(e.target.value)}
        className="min-h-[48px] w-full rounded border border-input bg-background px-2 py-1 font-mono text-xs"
        placeholder="ssh-ed25519 AAAA..."
        autoComplete="off"
      />
      {error && (
        <p className="font-mono text-xs text-destructive">
          {t('robots.detail.rotate.pubCompareError')}: {error}
        </p>
      )}
      {computed && (
        <p className="font-mono text-xs text-muted-foreground">
          {t('robots.detail.rotate.fingerprint')}: {computed.fingerprint}{' '}
          <span className="text-[10px]">({computed.keyType})</span>
        </p>
      )}
      <button
        type="button"
        onClick={() => {
          setOpen(false)
          setPubKey('')
        }}
        className="text-[11px] text-muted-foreground underline hover:no-underline"
      >
        {t('robots.detail.rotate.pubCompareHide')}
      </button>
    </div>
  )
}

// computePubKeyFingerprint — OpenSSH `ssh-<algo> <base64-data> <comment>` format에서
// SHA256 fingerprint 계산. ssh-keygen -lf 와 일치하는 표준 표현 ("SHA256:<base64-no-pad>").
async function computePubKeyFingerprint(
  pubKey: string,
): Promise<{ fingerprint: string; keyType: string }> {
  const parts = pubKey.split(/\s+/).filter(Boolean)
  if (parts.length < 2) {
    throw new Error('expected: <type> <base64-data> [comment]')
  }
  const keyType = parts[0]!
  const base64Data = parts[1]!
  let raw: Uint8Array
  try {
    raw = Uint8Array.from(atob(base64Data), (c) => c.charCodeAt(0))
  } catch {
    throw new Error('invalid base64 in public key')
  }
  const digest = await crypto.subtle.digest('SHA-256', raw.buffer as ArrayBuffer)
  const bytes = new Uint8Array(digest)
  let bin = ''
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]!)
  const fingerprint = 'SHA256:' + btoa(bin).replace(/=+$/, '')
  return { fingerprint, keyType }
}

// silence unused import (Robot type — referenced via useRobot return).
const _typeRef: undefined | Robot = undefined
void _typeRef

export const Route = createFileRoute('/_authenticated/robots/$robotId')({
  component: RobotDetailPage,
})
