import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'

import {
  useDeleteRobot,
  useFleet,
  useIsAdmin,
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
function RobotResultsCard({ robotId }: { robotId: string }): React.ReactElement {
  const t = useT()
  const q = useRobotResults(robotId, 20)
  const results = q.data ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('robots.detail.results.title')}</CardTitle>
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
            {groupBySession(results).map((group) => (
              <SessionGroup key={group.sessionId} group={group} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
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
}: {
  group: SessionResultGroup
}): React.ReactElement {
  const t = useT()
  return (
    <div className="space-y-1">
      <div className="flex items-baseline gap-2 text-xs text-muted-foreground">
        <span>{t('robots.detail.results.session')}:</span>
        <Link
          to="/scans"
          search={{ session: group.sessionId }}
          className="font-mono hover:text-foreground hover:underline"
        >
          {group.sessionId}
        </Link>
        <span className="ml-auto">
          {t('robots.detail.results.count', { count: group.results.length })}
        </span>
      </div>
      <div className="space-y-1">
        {group.results.map((r) => (
          <ResultRow key={r.id} result={r} />
        ))}
      </div>
    </div>
  )
}

function ResultRow({
  result,
}: {
  result: RobotResult
}): React.ReactElement {
  const packKey = result.packKey
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
                <PemFingerprintPreview pem={privateKeyPem} />
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

// PemFingerprintPreview — pem 입력 시 SHA256 hash를 비동기 계산해 첫 16 hex + 마지막 4로 표시.
//
// 주의: SSH 표준 fingerprint는 공개키 SHA256 (private→public 추출 필요). 본 컴포넌트는
// **input bytes SHA256**으로 visual confirmation 용도 — pasted PEM이 의도한 것인지 확인하는
// 데 충분. 실 SSH fingerprint(공개키 기반)는 별 epic.
function PemFingerprintPreview({ pem }: { pem: string }): React.ReactElement | null {
  const t = useT()
  const [fingerprint, setFingerprint] = useState('')
  useEffect(() => {
    const trimmed = pem.trim()
    if (!trimmed) {
      setFingerprint('')
      return
    }
    let cancelled = false
    const compute = async () => {
      try {
        const enc = new TextEncoder().encode(trimmed)
        const buf = await crypto.subtle.digest('SHA-256', enc)
        const hex = Array.from(new Uint8Array(buf))
          .map((b) => b.toString(16).padStart(2, '0'))
          .join('')
        if (!cancelled) setFingerprint(hex)
      } catch {
        if (!cancelled) setFingerprint('')
      }
    }
    void compute()
    return () => {
      cancelled = true
    }
  }, [pem])

  if (!fingerprint) return null
  // 첫 16 hex + ... + 마지막 4 (SHA256 fingerprint truncated 표기 관행).
  const short = `${fingerprint.slice(0, 16)}…${fingerprint.slice(-4)}`
  return (
    <p className="font-mono text-xs text-muted-foreground">
      {t('robots.detail.rotate.fingerprint')}: SHA256:{short}
    </p>
  )
}

// silence unused import (Robot type — referenced via useRobot return).
const _typeRef: undefined | Robot = undefined
void _typeRef

export const Route = createFileRoute('/_authenticated/robots/$robotId')({
  component: RobotDetailPage,
})
