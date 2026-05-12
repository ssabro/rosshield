import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'

import { apiClient } from '@/api/client'
import { extractErrorMessage } from '@/api/errors'
import {
  useDeleteRobot,
  useFleet,
  useIsAdmin,
  usePacks,
  useRobot,
  useRobotResults,
  useRotateCredential,
} from '@/api/hooks'
import { Breadcrumbs } from '@/components/layout/Breadcrumbs'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
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

import type { FormEvent } from 'react'

import type { Robot, RobotResult } from '@/api/hooks'

// `/robots/$robotId` — 단일 robot 상세 (모든 인증 사용자).
function RobotDetailPage(): React.ReactElement {
  const t = useT()
  const { robotId } = Route.useParams()
  const robotQuery = useRobot(robotId)
  const robot = robotQuery.data
  // fleetId는 robot fetch 후에만 알 수 있음 — useFleet은 enabled !!fleetId.
  const fleetQuery = useFleet(robot?.fleetId)

  if (robotQuery.isPending) {
    return (
      <div className="space-y-6">
        <PageHeader title={t('pages.robots.title')} />
        <p className="text-sm text-muted-foreground">{t('robots.detail.loading')}</p>
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

  return (
    <div className="space-y-6">
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
      <PageHeader title={robot.name} description={role} />

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

      <RotateCredentialCard robotId={robot.id} />

      <DeleteRobotCard robot={robot} />

      <p className="text-xs text-muted-foreground">
        <Link to="/robots" className="underline">
          {t('robots.detail.back')}
        </Link>
      </p>
    </div>
  )
}

// DeleteRobotCard — admin only, 2-step confirm. 성공 시 /robots로 navigate.
function DeleteRobotCard({ robot }: { robot: Robot }): React.ReactElement | null {
  const t = useT()
  const isAdmin = useIsAdmin()
  const navigate = useNavigate()
  const del = useDeleteRobot()
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState('')

  if (!isAdmin) return null

  if (confirming) {
    return (
      <Card className="max-w-xl border-destructive">
        <CardHeader>
          <CardTitle className="text-sm text-destructive">
            {t('robots.detail.delete.confirmTitle')}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <p>{t('robots.detail.delete.confirmBody')}</p>
          <div className="flex items-center gap-2">
            <Button
              variant="destructive"
              size="sm"
              disabled={del.isPending}
              onClick={() =>
                del.mutate(robot.id, {
                  onSuccess: () => {
                    void navigate({ to: '/robots', replace: true })
                  },
                  onError: (e) =>
                    setError(e instanceof Error ? e.message : t('robots.detail.delete.error')),
                })
              }
            >
              {del.isPending
                ? t('robots.detail.delete.pending')
                : t('robots.detail.delete.yes')}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setConfirming(false)
                setError('')
              }}
            >
              {t('robots.detail.delete.cancel')}
            </Button>
            {error && <span className="text-xs text-destructive">{error}</span>}
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card className="max-w-xl">
      <CardHeader>
        <CardTitle className="text-sm">{t('robots.detail.delete.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        <Button variant="destructive" size="sm" onClick={() => setConfirming(true)}>
          {t('robots.detail.delete.button')}
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
        <div
          className="rounded border border-destructive/30 bg-destructive/5 px-2 py-1 text-xs text-destructive"
          title={failureReason}
        >
          <span className="font-medium">
            {t('robots.detail.results.failureReason')}:
          </span>{' '}
          <span className="break-words">{failureReason}</span>
        </div>
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
// completed=default(녹색 강조)·failed=destructive·running=secondary·나머지=outline.
function sessionStatusVariant(
  status: string,
): 'default' | 'destructive' | 'secondary' | 'outline' {
  switch (status) {
    case 'completed':
      return 'default'
    case 'failed':
      return 'destructive'
    case 'running':
      return 'secondary'
    case 'cancelled':
    case 'pending':
    default:
      return 'outline'
  }
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

// RotateCredentialCard — admin only. 평문 자격증명 입력 → 도메인 KEK 재wrap. 성공 시 성공 메시지 + 폼 초기화.
function RotateCredentialCard({
  robotId,
}: {
  robotId: string
}): React.ReactElement | null {
  const t = useT()
  const isAdmin = useIsAdmin()
  const rotate = useRotateCredential()
  const [open, setOpen] = useState(false)
  const [authType, setAuthType] = useState<'password' | 'privateKey'>('password')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [privateKeyPem, setPrivateKeyPem] = useState('')
  const [privateKeyPassphrase, setPrivateKeyPassphrase] = useState('')
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')

  if (!isAdmin) return null

  const reset = () => {
    setAuthType('password')
    setUsername('')
    setPassword('')
    setPrivateKeyPem('')
    setPrivateKeyPassphrase('')
    setError('')
  }

  const handleSubmit = (e: FormEvent<HTMLFormElement>): void => {
    e.preventDefault()
    setError('')
    setSuccess('')
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
          setSuccess(
            t('robots.detail.rotate.success', { id: data.newCredentialId }),
          )
          reset()
          setOpen(false)
        },
        onError: (e) => setError(e instanceof Error ? e.message : t('robots.detail.rotate.error')),
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
        <form onSubmit={handleSubmit} className="space-y-3 text-sm">
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

// silence unused import (Robot type — referenced via useRobot return).
const _typeRef: undefined | Robot = undefined
void _typeRef

export const Route = createFileRoute('/_authenticated/robots/$robotId')({
  component: RobotDetailPage,
})
