import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { ClipboardCheck, Inbox } from 'lucide-react'

import { ApiError } from '@/api/errors'
import {
  useComplianceProfiles,
  useComplianceSnapshots,
  useCreateComplianceProfile,
  useGenerateSnapshot,
  useHasPermission,
} from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { mutationGuardTitle, useIsOffline } from '@/lib/use-is-offline'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { CardSkeleton, TableRowSkeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type {
  ComplianceControlStatus,
  ComplianceProfile,
  ComplianceSnapshot,
  CreateComplianceProfileVars,
} from '@/api/hooks'

// `/compliance` — 컴플라이언스 프로필 관리 (E19-2).
// - 상단: 새 프로필 활성화 폼 (framework + version)
// - 중간: 프로필 목록 (행 클릭 → 선택)
// - 하단: 선택 프로필의 snapshot 목록 + "스냅샷 생성" 폼 (sessionId)
const FRAMEWORKS: Array<{
  value: CreateComplianceProfileVars['framework']
  labelKey:
    | 'compliance.framework.isms-p'
    | 'compliance.framework.iso27001-2022'
    | 'compliance.framework.nist-800-53-rev5'
}> = [
  { value: 'isms-p', labelKey: 'compliance.framework.isms-p' },
  { value: 'iso27001-2022', labelKey: 'compliance.framework.iso27001-2022' },
  { value: 'nist-800-53-rev5', labelKey: 'compliance.framework.nist-800-53-rev5' },
]

function CompliancePage(): React.ReactElement {
  const profiles = useComplianceProfiles()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const t = useT()

  const selected = profiles.data?.find((p) => p.id === selectedId) ?? null

  // D-UI-1 Stage 4 — PageHeader subtitle 보강: 활성 framework 카운트 + 선택된 framework 표시.
  // 현재 활성 framework이 있으면 PageHeader 우측에 Badge로 표시 (visual hierarchy 강화).
  const activeFrameworks = profiles.data?.filter((p) => p.enabled) ?? []
  const headerBadge =
    activeFrameworks.length > 0 ? (
      <Badge variant="secondary" className="text-[10px] font-normal">
        {t('compliance.header.activeBadge', {
          count: activeFrameworks.length.toString(),
        })}
      </Badge>
    ) : undefined

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('pages.compliance.title')}
        description={t('pages.compliance.description')}
        badge={headerBadge}
      />

      <CreateProfileForm />

      <section className="space-y-2">
        <h2 className="text-lg font-medium">{t('compliance.profile.section')}</h2>
        <div className="overflow-x-auto rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('compliance.profile.table.framework')}</TableHead>
                <TableHead>{t('compliance.profile.table.version')}</TableHead>
                <TableHead>{t('compliance.profile.table.status')}</TableHead>
                <TableHead>{t('compliance.profile.table.created')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {profiles.isPending && (
                <TableRow>
                  <TableCell colSpan={4} className="p-3">
                    <TableRowSkeleton rows={3} columns={4} />
                  </TableCell>
                </TableRow>
              )}
              {profiles.isError && (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-destructive">
                    {profiles.error instanceof ApiError
                      ? profiles.error.message
                      : t('compliance.profile.list.error')}
                  </TableCell>
                </TableRow>
              )}
              {profiles.isSuccess && profiles.data.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="p-0">
                    <EmptyState
                      icon={ClipboardCheck}
                      title={t('compliance.profile.empty.title')}
                      description={t('compliance.profile.empty.description')}
                      className="rounded-none border-0 bg-transparent"
                    />
                  </TableCell>
                </TableRow>
              )}
              {profiles.isSuccess &&
                profiles.data.map((p) => (
                  <ProfileRow
                    key={p.id}
                    profile={p}
                    selected={selectedId === p.id}
                    onSelect={() => setSelectedId(p.id)}
                  />
                ))}
            </TableBody>
          </Table>
        </div>
      </section>

      {selected && <SnapshotsSection profile={selected} />}
    </div>
  )
}

function ProfileRow({
  profile,
  selected,
  onSelect,
}: {
  profile: ComplianceProfile
  selected: boolean
  onSelect: () => void
}): React.ReactElement {
  const t = useT()
  return (
    <TableRow
      onClick={onSelect}
      className={
        selected
          ? 'cursor-pointer bg-accent text-accent-foreground'
          : 'cursor-pointer hover:bg-muted/50'
      }
    >
      <TableCell className="font-medium">{profile.framework}</TableCell>
      <TableCell className="font-mono text-xs">{profile.frameworkVersion}</TableCell>
      <TableCell>
        <Badge variant={profile.enabled ? 'default' : 'secondary'}>
          {profile.enabled
            ? t('compliance.profile.status.enabled')
            : t('compliance.profile.status.disabled')}
        </Badge>
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {new Date(profile.createdAt).toLocaleString()}
      </TableCell>
    </TableRow>
  )
}

function CreateProfileForm(): React.ReactElement {
  const [framework, setFramework] = useState<
    CreateComplianceProfileVars['framework'] | ''
  >('')
  const [version, setVersion] = useState('')
  const create = useCreateComplianceProfile()
  const t = useT()
  // RBAC Stage 5 — compliance profile 생성은 tenant compliance.admin (§2.2 ID 17).
  const canCreate = useHasPermission('compliance', 'admin')
  const isOffline = useIsOffline()

  const onSubmit = (e: React.FormEvent): void => {
    e.preventDefault()
    if (!framework || !version.trim()) return
    create.mutate(
      { framework, frameworkVersion: version.trim() },
      {
        onSuccess: () => {
          setFramework('')
          setVersion('')
        },
      },
    )
  }

  return (
    <form
      onSubmit={onSubmit}
      className="grid grid-cols-1 items-end gap-3 rounded-md border p-4 md:grid-cols-[1fr_1fr_auto]"
    >
      <div className="flex flex-col gap-2">
        <Label htmlFor="framework">{t('compliance.profile.framework')}</Label>
        <Select
          value={framework}
          onValueChange={(v) =>
            setFramework(v as CreateComplianceProfileVars['framework'])
          }
        >
          <SelectTrigger id="framework">
            <SelectValue placeholder={t('compliance.profile.framework.placeholder')} />
          </SelectTrigger>
          <SelectContent>
            {FRAMEWORKS.map((f) => (
              <SelectItem key={f.value} value={f.value}>
                {t(f.labelKey)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="version">{t('compliance.profile.version')}</Label>
        <Input
          id="version"
          placeholder={t('compliance.profile.version.placeholder')}
          value={version}
          onChange={(e) => setVersion(e.target.value)}
        />
      </div>
      <Button
        type="submit"
        disabled={
          !framework ||
          !version.trim() ||
          create.isPending ||
          !canCreate ||
          isOffline
        }
        title={mutationGuardTitle({
          isOffline,
          offlineLabel: t('pwa.offline.mutationBlocked'),
          fallback: !canCreate ? t('common.role.required.admin') : undefined,
        })}
      >
        {create.isPending
          ? t('compliance.profile.adding')
          : t('compliance.profile.add')}
      </Button>
      {create.isError && (
        <p className="text-sm text-destructive md:col-span-3">
          {create.error instanceof ApiError
            ? create.error.message
            : t('compliance.profile.error.fallback')}
        </p>
      )}
    </form>
  )
}

function SnapshotsSection({
  profile,
}: {
  profile: ComplianceProfile
}): React.ReactElement {
  const snapshots = useComplianceSnapshots(profile.id)
  const latest = snapshots.data?.[0]
  const t = useT()

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-lg font-medium">{t('compliance.snapshot.section')}</h2>
        <p className="text-xs text-muted-foreground">
          {t('compliance.snapshot.selected')}{' '}
          <Badge variant="outline" className="ml-1 font-mono text-[10px]">
            {profile.framework} · {profile.frameworkVersion}
          </Badge>
        </p>
      </div>

      {snapshots.isPending && !latest && <CardSkeleton />}
      {latest && <ScoreHeroCard snapshot={latest} />}
      {latest && <ControlsBreakdown snapshot={latest} />}

      <GenerateSnapshotForm profileId={profile.id} />

      <div className="overflow-x-auto rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('compliance.snapshot.table.score')}</TableHead>
              <TableHead>{t('compliance.snapshot.table.pass')}</TableHead>
              <TableHead>{t('compliance.snapshot.table.fail')}</TableHead>
              <TableHead>{t('compliance.snapshot.table.partial')}</TableHead>
              <TableHead>{t('compliance.snapshot.table.na')}</TableHead>
              <TableHead>{t('compliance.snapshot.table.unmapped')}</TableHead>
              <TableHead>{t('compliance.snapshot.table.session')}</TableHead>
              <TableHead>{t('compliance.snapshot.table.created')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {snapshots.isPending && (
              <TableRow>
                <TableCell colSpan={8} className="p-3">
                  <TableRowSkeleton rows={3} columns={8} />
                </TableCell>
              </TableRow>
            )}
            {snapshots.isError && (
              <TableRow>
                <TableCell colSpan={8} className="text-center text-destructive">
                  {snapshots.error instanceof ApiError
                    ? snapshots.error.message
                    : t('compliance.snapshot.list.error')}
                </TableCell>
              </TableRow>
            )}
            {snapshots.isSuccess && snapshots.data.length === 0 && (
              <TableRow>
                <TableCell colSpan={8} className="p-0">
                  <EmptyState
                    icon={Inbox}
                    title={t('compliance.snapshot.empty.title')}
                    description={t('compliance.snapshot.empty.description')}
                    className="rounded-none border-0 bg-transparent"
                  />
                </TableCell>
              </TableRow>
            )}
            {snapshots.isSuccess &&
              snapshots.data.map((s) => <SnapshotRow key={s.id} snapshot={s} />)}
          </TableBody>
        </Table>
      </div>
    </section>
  )
}

function SnapshotRow({
  snapshot,
}: {
  snapshot: ComplianceSnapshot
}): React.ReactElement {
  return (
    <TableRow>
      <TableCell>
        <Badge variant={scoreVariant(snapshot.overallScore)}>
          {formatScore(snapshot.overallScore)}
        </Badge>
      </TableCell>
      <TableCell className="font-mono text-xs">{snapshot.passCount}</TableCell>
      <TableCell className="font-mono text-xs text-destructive">
        {snapshot.failCount}
      </TableCell>
      <TableCell className="font-mono text-xs">{snapshot.partialCount}</TableCell>
      <TableCell className="font-mono text-xs text-muted-foreground">
        {snapshot.notApplicableCount}
      </TableCell>
      <TableCell className="font-mono text-xs text-muted-foreground">
        {snapshot.unmappedCount}
      </TableCell>
      <TableCell
        className="max-w-[12rem] truncate font-mono text-xs text-muted-foreground"
        title={snapshot.sessionId}
      >
        {snapshot.sessionId || '-'}
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {new Date(snapshot.createdAt).toLocaleString()}
      </TableCell>
    </TableRow>
  )
}

function GenerateSnapshotForm({
  profileId,
}: {
  profileId: string
}): React.ReactElement {
  const [sessionId, setSessionId] = useState('')
  const generate = useGenerateSnapshot()
  const t = useT()
  // RBAC Stage 5 — snapshot 생성은 D-RBAC-4 권장 default = fleet[X].compliance.execute
  // (§2.2 ID 18). web에서 sessionId만 알고 fleet ID 매핑이 없으므로 보수적으로
  // tenant scope (admin/owner) 평가만 — fleet-admin은 server에서 fleet 일치 시 통과.
  // 향후 sessionId → fleetId resolve hook 추가 시 본 호출에 fleetId 전달.
  const canGenerate = useHasPermission('compliance', 'execute')
  const isOffline = useIsOffline()

  const onSubmit = (e: React.FormEvent): void => {
    e.preventDefault()
    if (!sessionId.trim()) return
    generate.mutate(
      { profileId, sessionId: sessionId.trim() },
      { onSuccess: () => setSessionId('') },
    )
  }

  return (
    <form
      onSubmit={onSubmit}
      className="grid grid-cols-1 items-end gap-3 rounded-md border p-4 md:grid-cols-[1fr_auto]"
    >
      <div className="flex flex-col gap-2">
        <Label htmlFor="session-id">{t('compliance.snapshot.session')}</Label>
        <Input
          id="session-id"
          placeholder={t('compliance.snapshot.session.placeholder')}
          value={sessionId}
          onChange={(e) => setSessionId(e.target.value)}
        />
      </div>
      <Button
        type="submit"
        disabled={
          !sessionId.trim() || generate.isPending || !canGenerate || isOffline
        }
        title={mutationGuardTitle({
          isOffline,
          offlineLabel: t('pwa.offline.mutationBlocked'),
          fallback: !canGenerate ? t('common.role.required.admin') : undefined,
        })}
      >
        {generate.isPending
          ? t('compliance.snapshot.generating')
          : t('compliance.snapshot.generate')}
      </Button>
      {generate.isError && (
        <p className="text-sm text-destructive md:col-span-2">
          {generate.error instanceof ApiError
            ? generate.error.message
            : t('compliance.snapshot.error.fallback')}
        </p>
      )}
    </form>
  )
}

function ScoreHeroCard({
  snapshot,
}: {
  snapshot: ComplianceSnapshot
}): React.ReactElement {
  const percent = Math.round(snapshot.overallScore * 100)
  const t = useT()
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('compliance.score.hero.title')}
        </CardTitle>
        <CardDescription className="text-xs">
          {new Date(snapshot.createdAt).toLocaleString()} · session{' '}
          <span className="font-mono">{snapshot.sessionId || '-'}</span>
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex items-baseline gap-3">
          <span className="text-4xl font-semibold tracking-tight">
            {formatScore(snapshot.overallScore)}
          </span>
          <Badge variant={scoreVariant(snapshot.overallScore)}>
            {t(scoreLabelKey(snapshot.overallScore))}
          </Badge>
        </div>
        <Progress value={percent} className="h-2" />
        <div className="grid grid-cols-2 gap-2 text-xs text-muted-foreground sm:grid-cols-5">
          <span>
            <span className="text-foreground">{t('compliance.snapshot.table.pass')}</span>{' '}
            {snapshot.passCount}
          </span>
          <span className="text-destructive">
            <span>{t('compliance.snapshot.table.fail')}</span> {snapshot.failCount}
          </span>
          <span>
            <span className="text-foreground">{t('compliance.snapshot.table.partial')}</span>{' '}
            {snapshot.partialCount}
          </span>
          <span>
            <span className="text-foreground">{t('compliance.snapshot.table.na')}</span>{' '}
            {snapshot.notApplicableCount}
          </span>
          <span>
            <span className="text-foreground">{t('compliance.snapshot.table.unmapped')}</span>{' '}
            {snapshot.unmappedCount}
          </span>
        </div>
      </CardContent>
    </Card>
  )
}

type StatusFilter =
  | 'all'
  | 'pass'
  | 'fail'
  | 'partial'
  | 'not_applicable'
  | 'unmapped'

// STATUS_FILTER_KEYS는 control status를 i18n dict 키로 매핑한다 (필터 pill + Badge 라벨 공통).
const STATUS_FILTER_KEYS: Record<
  Exclude<StatusFilter, 'all'>,
  | 'compliance.controls.filter.pass'
  | 'compliance.controls.filter.fail'
  | 'compliance.controls.filter.partial'
  | 'compliance.controls.filter.na'
  | 'compliance.controls.filter.unmapped'
> = {
  pass: 'compliance.controls.filter.pass',
  fail: 'compliance.controls.filter.fail',
  partial: 'compliance.controls.filter.partial',
  not_applicable: 'compliance.controls.filter.na',
  unmapped: 'compliance.controls.filter.unmapped',
}

function ControlsBreakdown({
  snapshot,
}: {
  snapshot: ComplianceSnapshot
}): React.ReactElement {
  const [filter, setFilter] = useState<StatusFilter>('all')
  const statuses = snapshot.statuses ?? []
  const t = useT()

  if (statuses.length === 0) {
    return (
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium text-muted-foreground">
            {t('compliance.controls.title')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <EmptyState
            icon={Inbox}
            title={t('compliance.controls.empty.title')}
            description={t('compliance.controls.empty.description')}
            className="bg-transparent"
          />
        </CardContent>
      </Card>
    )
  }

  const filtered =
    filter === 'all' ? statuses : statuses.filter((s) => s.status === filter)
  const grouped = groupByCategory(filtered)
  const counts = countByStatus(statuses)

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {t('compliance.controls.title')}
        </CardTitle>
        <CardDescription className="text-xs">
          {t('compliance.controls.subtitle', {
            total: statuses.length,
            groups: Object.keys(grouped).length,
          })}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap gap-1.5">
          <FilterPill
            active={filter === 'all'}
            onClick={() => setFilter('all')}
            label={t('compliance.controls.filter.all')}
            count={statuses.length}
          />
          {(
            Object.keys(STATUS_FILTER_KEYS) as Array<
              keyof typeof STATUS_FILTER_KEYS
            >
          ).map((s) => (
            <FilterPill
              key={s}
              active={filter === s}
              onClick={() => setFilter(s)}
              label={t(STATUS_FILTER_KEYS[s])}
              count={counts[s] ?? 0}
              tone={s}
            />
          ))}
        </div>

        {Object.keys(grouped).length === 0 ? (
          <p className="text-xs text-muted-foreground">
            {t('compliance.controls.no_match')}
          </p>
        ) : (
          <div className="space-y-3">
            {Object.entries(grouped).map(([category, items]) => (
              <div key={category} className="space-y-1.5">
                <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
                  <span>{category}</span>
                  <span className="text-[10px] font-normal">{items.length}</span>
                </div>
                <ul className="divide-y divide-border rounded-md border">
                  {items.map((s) => (
                    <ControlRow key={s.controlId} status={s} />
                  ))}
                </ul>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function ControlRow({
  status,
}: {
  status: ComplianceControlStatus
}): React.ReactElement {
  const t = useT()
  const labelKey = STATUS_FILTER_KEYS[status.status as keyof typeof STATUS_FILTER_KEYS]
  const label = labelKey ? t(labelKey) : status.status
  return (
    <li className="flex items-center gap-3 px-3 py-2 text-xs">
      <Badge
        variant={statusBadgeVariant(status.status)}
        className="shrink-0 text-[10px]"
      >
        {label}
      </Badge>
      <span className="font-mono text-foreground">{status.controlId}</span>
      <span className="ml-auto flex items-center gap-3 font-mono text-muted-foreground">
        <span className="text-foreground">P {status.passCount}</span>
        <span className="text-destructive">F {status.failCount}</span>
      </span>
      {status.notes && (
        <span className="max-w-[16rem] truncate text-muted-foreground" title={status.notes}>
          {status.notes}
        </span>
      )}
    </li>
  )
}

function FilterPill({
  active,
  onClick,
  label,
  count,
  tone,
}: {
  active: boolean
  onClick: () => void
  label: string
  count: number
  tone?: keyof typeof STATUS_FILTER_KEYS
}): React.ReactElement {
  const toneClass = tone === 'fail' ? 'text-destructive' : 'text-foreground'
  return (
    <button
      type="button"
      onClick={onClick}
      className={
        active
          ? 'rounded-full bg-primary px-3 py-1 text-xs font-medium text-primary-foreground'
          : 'rounded-full border border-border bg-transparent px-3 py-1 text-xs hover:bg-muted'
      }
    >
      <span className={active ? '' : toneClass}>{label}</span>
      <span className="ml-1.5 text-[10px] opacity-80">{count}</span>
    </button>
  )
}

// statusBadgeVariant는 control status를 shadcn Badge variant로 매핑합니다.
// 단위 테스트(compliance.test.tsx) 대상으로 export.
export function statusBadgeVariant(
  status: string,
): 'default' | 'destructive' | 'secondary' | 'outline' {
  switch (status) {
    case 'pass':
      return 'default'
    case 'fail':
      return 'destructive'
    case 'partial':
      return 'secondary'
    default:
      return 'outline'
  }
}

// groupByCategory는 controlId의 첫 카테고리 prefix로 통제를 그룹화합니다.
// 예: "ISMS-P:1.1.1" → 카테고리 "ISMS-P:1", "CIS-1.1.1.1" → 카테고리 "CIS-1".
// 단위 테스트(compliance.test.tsx) 대상으로 export.
export function groupByCategory(
  statuses: ComplianceControlStatus[],
): Record<string, ComplianceControlStatus[]> {
  const out: Record<string, ComplianceControlStatus[]> = {}
  for (const s of statuses) {
    const key = categoryOf(s.controlId)
    if (!out[key]) out[key] = []
    out[key].push(s)
  }
  return out
}

function categoryOf(controlId: string): string {
  // "ISMS-P:1.1.1" → "ISMS-P:1"; "CIS-1.1.1.1" → "CIS-1"; "X" → "X"
  const colonIdx = controlId.indexOf(':')
  if (colonIdx >= 0) {
    const prefix = controlId.slice(0, colonIdx)
    const rest = controlId.slice(colonIdx + 1)
    const dotIdx = rest.indexOf('.')
    if (dotIdx > 0) return `${prefix}:${rest.slice(0, dotIdx)}`
    return controlId
  }
  const dotIdx = controlId.indexOf('.')
  if (dotIdx > 0) return controlId.slice(0, dotIdx)
  return controlId
}

function countByStatus(
  statuses: ComplianceControlStatus[],
): Record<string, number> {
  const out: Record<string, number> = {}
  for (const s of statuses) {
    out[s.status] = (out[s.status] ?? 0) + 1
  }
  return out
}

// scoreLabelKey는 점수에 대응하는 i18n dict 키를 반환한다.
// ≥0.9 우수 / ≥0.7 양호 / else 미흡. 호출자가 t()로 번역.
export function scoreLabelKey(
  score: number,
):
  | 'compliance.score.label.excellent'
  | 'compliance.score.label.good'
  | 'compliance.score.label.poor' {
  if (score >= 0.9) return 'compliance.score.label.excellent'
  if (score >= 0.7) return 'compliance.score.label.good'
  return 'compliance.score.label.poor'
}

// scoreVariant는 overall_score를 shadcn Badge variant로 매핑합니다.
// ≥0.9 default(녹색-ish), ≥0.7 secondary(중립), 그 외 destructive(빨강).
// 단위 테스트(compliance.test.tsx) 대상으로 export.
export function scoreVariant(
  score: number,
): 'default' | 'destructive' | 'secondary' | 'outline' {
  if (score >= 0.9) return 'default'
  if (score >= 0.7) return 'secondary'
  return 'destructive'
}

// formatScore는 overall_score(0~1)를 사용자 가시 문자열("83.4%")로 변환합니다.
// 단위 테스트(compliance.test.tsx) 대상으로 export.
export function formatScore(score: number): string {
  return `${(score * 100).toFixed(1)}%`
}

export const Route = createFileRoute('/_authenticated/compliance')({
  component: CompliancePage,
})
