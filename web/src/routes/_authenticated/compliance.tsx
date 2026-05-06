import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { ClipboardCheck, Inbox } from 'lucide-react'

import { ApiError } from '@/api/errors'
import {
  useComplianceProfiles,
  useComplianceSnapshots,
  useCreateComplianceProfile,
  useGenerateSnapshot,
} from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type {
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
  label: string
}> = [
  { value: 'isms-p', label: 'ISMS-P' },
  { value: 'iso27001-2022', label: 'ISO 27001:2022' },
  { value: 'nist-800-53-rev5', label: 'NIST 800-53 Rev.5' },
]

function CompliancePage(): React.ReactElement {
  const profiles = useComplianceProfiles()
  const [selectedId, setSelectedId] = useState<string | null>(null)

  const selected = profiles.data?.find((p) => p.id === selectedId) ?? null

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Compliance</h1>
        <p className="text-sm text-muted-foreground">
          프레임워크별 프로필을 활성화하고, 스캔 세션 결과로부터 스냅샷을 생성합니다.
        </p>
      </div>

      <CreateProfileForm />

      <section className="space-y-2">
        <h2 className="text-lg font-medium">프로필</h2>
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Framework</TableHead>
                <TableHead>버전</TableHead>
                <TableHead>상태</TableHead>
                <TableHead>생성</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {profiles.isPending && (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-muted-foreground">
                    불러오는 중…
                  </TableCell>
                </TableRow>
              )}
              {profiles.isError && (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-destructive">
                    {profiles.error instanceof ApiError
                      ? profiles.error.message
                      : '프로필 목록을 불러올 수 없습니다'}
                  </TableCell>
                </TableRow>
              )}
              {profiles.isSuccess && profiles.data.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="p-0">
                    <EmptyState
                      icon={ClipboardCheck}
                      title="활성 프로필이 없습니다"
                      description="위 폼에서 프레임워크와 버전을 선택해 첫 프로필을 활성화하세요."
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
          {profile.enabled ? 'enabled' : 'disabled'}
        </Badge>
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {new Date(profile.createdAt).toLocaleString('ko-KR')}
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
        <Label htmlFor="framework">Framework</Label>
        <Select
          value={framework}
          onValueChange={(v) =>
            setFramework(v as CreateComplianceProfileVars['framework'])
          }
        >
          <SelectTrigger id="framework">
            <SelectValue placeholder="선택" />
          </SelectTrigger>
          <SelectContent>
            {FRAMEWORKS.map((f) => (
              <SelectItem key={f.value} value={f.value}>
                {f.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="version">Framework 버전</Label>
        <Input
          id="version"
          placeholder="예: 2023.1"
          value={version}
          onChange={(e) => setVersion(e.target.value)}
        />
      </div>
      <Button
        type="submit"
        disabled={!framework || !version.trim() || create.isPending}
      >
        {create.isPending ? '추가 중…' : '프로필 추가'}
      </Button>
      {create.isError && (
        <p className="text-sm text-destructive md:col-span-3">
          {create.error instanceof ApiError
            ? create.error.message
            : '프로필 추가에 실패했습니다'}
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

  return (
    <section className="space-y-3">
      <div className="flex items-baseline justify-between">
        <h2 className="text-lg font-medium">스냅샷</h2>
        <p className="text-xs text-muted-foreground">
          선택: <span className="font-mono">{profile.framework}</span>{' '}
          <span className="font-mono">{profile.frameworkVersion}</span>
        </p>
      </div>

      {latest && <ScoreHeroCard snapshot={latest} />}

      <GenerateSnapshotForm profileId={profile.id} />

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Score</TableHead>
              <TableHead>Pass</TableHead>
              <TableHead>Fail</TableHead>
              <TableHead>Partial</TableHead>
              <TableHead>N/A</TableHead>
              <TableHead>Unmapped</TableHead>
              <TableHead>Session</TableHead>
              <TableHead>생성</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {snapshots.isPending && (
              <TableRow>
                <TableCell colSpan={8} className="text-center text-muted-foreground">
                  불러오는 중…
                </TableCell>
              </TableRow>
            )}
            {snapshots.isError && (
              <TableRow>
                <TableCell colSpan={8} className="text-center text-destructive">
                  {snapshots.error instanceof ApiError
                    ? snapshots.error.message
                    : '스냅샷 목록을 불러올 수 없습니다'}
                </TableCell>
              </TableRow>
            )}
            {snapshots.isSuccess && snapshots.data.length === 0 && (
              <TableRow>
                <TableCell colSpan={8} className="p-0">
                  <EmptyState
                    icon={Inbox}
                    title="스냅샷이 없습니다"
                    description="위 폼에서 scan session ID를 입력해 첫 스냅샷을 생성하세요."
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
        {new Date(snapshot.createdAt).toLocaleString('ko-KR')}
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
        <Label htmlFor="session-id">스캔 Session ID</Label>
        <Input
          id="session-id"
          placeholder="예: ss_..."
          value={sessionId}
          onChange={(e) => setSessionId(e.target.value)}
        />
      </div>
      <Button type="submit" disabled={!sessionId.trim() || generate.isPending}>
        {generate.isPending ? '생성 중…' : '스냅샷 생성'}
      </Button>
      {generate.isError && (
        <p className="text-sm text-destructive md:col-span-2">
          {generate.error instanceof ApiError
            ? generate.error.message
            : '스냅샷 생성에 실패했습니다'}
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
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          최근 스냅샷 점수
        </CardTitle>
        <CardDescription className="text-xs">
          {new Date(snapshot.createdAt).toLocaleString('ko-KR')} · session{' '}
          <span className="font-mono">{snapshot.sessionId || '-'}</span>
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex items-baseline gap-3">
          <span className="text-4xl font-semibold tracking-tight">
            {formatScore(snapshot.overallScore)}
          </span>
          <Badge variant={scoreVariant(snapshot.overallScore)}>
            {scoreLabel(snapshot.overallScore)}
          </Badge>
        </div>
        <Progress value={percent} className="h-2" />
        <div className="grid grid-cols-2 gap-2 text-xs text-muted-foreground sm:grid-cols-5">
          <span>
            <span className="text-foreground">Pass</span> {snapshot.passCount}
          </span>
          <span className="text-destructive">
            <span>Fail</span> {snapshot.failCount}
          </span>
          <span>
            <span className="text-foreground">Partial</span>{' '}
            {snapshot.partialCount}
          </span>
          <span>
            <span className="text-foreground">N/A</span>{' '}
            {snapshot.notApplicableCount}
          </span>
          <span>
            <span className="text-foreground">Unmapped</span>{' '}
            {snapshot.unmappedCount}
          </span>
        </div>
      </CardContent>
    </Card>
  )
}

function scoreLabel(score: number): string {
  if (score >= 0.9) return '우수'
  if (score >= 0.7) return '양호'
  return '미흡'
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
