import { createFileRoute, Link } from '@tanstack/react-router'
import { useState } from 'react'

import { Server } from 'lucide-react'

import { ApiError } from '@/api/errors'
import { useCreateRobot, useIsAdmin, useRobots } from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { Badge } from '@/components/ui/badge'
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type { CreateRobotVars, Robot } from '@/api/hooks'

// `/robots` — 로봇 목록 + fleet 필터.
// - 빈 결과: "(로봇 없음)"
// - 로딩: "불러오는 중…"
// - 에러: ApiError 메시지 표시
// 컬럼: 이름·호스트:포트·인증·심각도·태그
function RobotsPage(): React.ReactElement {
  const [fleetId, setFleetId] = useState('')
  const [showForm, setShowForm] = useState(false)
  const trimmed = fleetId.trim()
  const robots = useRobots(trimmed.length > 0 ? trimmed : undefined)
  const t = useT()
  const isAdmin = useIsAdmin()

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.robots.title')}
        description={t('pages.robots.description')}
        actions={
          <Button
            variant={showForm ? 'outline' : 'default'}
            size="sm"
            onClick={() => setShowForm((v) => !v)}
            disabled={!isAdmin}
            title={!isAdmin ? t('common.role.required.admin') : undefined}
          >
            {showForm ? t('robots.form.toggle.hide') : t('robots.form.toggle.show')}
          </Button>
        }
      />

      {showForm && isAdmin && <CreateRobotForm onCreated={() => setShowForm(false)} />}

      <div className="flex max-w-sm flex-col gap-2">
        <Label htmlFor="fleet-filter">{t('robots.filter.fleet')}</Label>
        <Input
          id="fleet-filter"
          placeholder={t('robots.filter.fleet.placeholder')}
          value={fleetId}
          onChange={(e) => setFleetId(e.target.value)}
        />
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('robots.table.name')}</TableHead>
              <TableHead>{t('robots.table.host')}</TableHead>
              <TableHead>{t('robots.table.auth')}</TableHead>
              <TableHead>{t('robots.table.criticality')}</TableHead>
              <TableHead>{t('robots.table.tags')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {robots.isPending && (
              <TableRow>
                <TableCell
                  colSpan={5}
                  className="text-center text-muted-foreground"
                >
                  {t('common.loading')}
                </TableCell>
              </TableRow>
            )}
            {robots.isError && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-destructive">
                  {robots.error instanceof ApiError
                    ? robots.error.message
                    : t('robots.error.fallback')}
                </TableCell>
              </TableRow>
            )}
            {robots.isSuccess && robots.data.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="p-0">
                  <EmptyState
                    icon={Server}
                    title={t('robots.empty.title')}
                    description={t('robots.empty.description')}
                    className="rounded-none border-0 bg-transparent"
                  />
                </TableCell>
              </TableRow>
            )}
            {robots.isSuccess &&
              robots.data.map((robot) => (
                <RobotRow key={robot.id} robot={robot} />
              ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function RobotRow({ robot }: { robot: Robot }): React.ReactElement {
  const tags = Array.isArray(robot.tags) ? (robot.tags as unknown[]) : []
  return (
    <TableRow>
      <TableCell className="font-medium">
        <Link
          to="/robots/$robotId"
          params={{ robotId: robot.id }}
          className="hover:underline"
        >
          {robot.name}
        </Link>
      </TableCell>
      <TableCell className="font-mono text-xs">
        {robot.host}:{robot.port}
      </TableCell>
      <TableCell>{robot.authType}</TableCell>
      <TableCell>
        <Badge variant="secondary">{robot.criticality}</Badge>
      </TableCell>
      <TableCell>
        <div className="flex flex-wrap gap-1">
          {tags.length === 0 ? (
            <span className="text-xs text-muted-foreground">-</span>
          ) : (
            tags.map((tag, i) => (
              <Badge key={i} variant="outline">
                {String(tag)}
              </Badge>
            ))
          )}
        </div>
      </TableCell>
    </TableRow>
  )
}

// CreateRobotForm — 신규 Robot 등록 폼.
//   - 평문 자격증명을 본문으로 보냄 (백엔드가 KEK→DEK wrap).
//   - 성공 시 robots 캐시 무효화 → 자동 refetch + 폼 닫음.
function CreateRobotForm({
  onCreated,
}: {
  onCreated: () => void
}): React.ReactElement {
  const t = useT()
  const create = useCreateRobot()
  const isAdmin = useIsAdmin()
  const [vars, setVars] = useState<CreateRobotVars>({
    fleetId: '',
    name: '',
    host: '',
    port: 22,
    authType: 'password',
    username: '',
    password: '',
    criticality: 'medium',
  })
  const [tagsRaw, setTagsRaw] = useState('')
  const [success, setSuccess] = useState('')

  const set = <K extends keyof CreateRobotVars>(k: K, v: CreateRobotVars[K]): void => {
    setVars((prev) => ({ ...prev, [k]: v }))
  }

  const handleSubmit = (e: React.FormEvent): void => {
    e.preventDefault()
    setSuccess('')
    const tags = tagsRaw
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
    create.mutate(
      { ...vars, tags: tags.length > 0 ? tags : undefined },
      {
        onSuccess: (data) => {
          setSuccess(t('robots.form.success', { id: data.robot.id }))
          // 본 폼은 한 번 닫고 새로 열도록 — 입력 유지하지 않음.
          onCreated()
        },
      },
    )
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="grid grid-cols-1 gap-3 rounded-md border p-4 md:grid-cols-2"
      aria-label={t('robots.form.title')}
    >
      <div className="md:col-span-2">
        <h3 className="text-sm font-medium">{t('robots.form.title')}</h3>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="rb-fleet">{t('robots.form.fleet')}</Label>
        <Input
          id="rb-fleet"
          required
          placeholder={t('robots.form.fleet.placeholder')}
          value={vars.fleetId}
          onChange={(e) => set('fleetId', e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="rb-name">{t('robots.form.name')}</Label>
        <Input
          id="rb-name"
          required
          placeholder={t('robots.form.name.placeholder')}
          value={vars.name}
          onChange={(e) => set('name', e.target.value)}
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="rb-host">{t('robots.form.host')}</Label>
        <Input
          id="rb-host"
          required
          placeholder={t('robots.form.host.placeholder')}
          value={vars.host}
          onChange={(e) => set('host', e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="rb-port">{t('robots.form.port')}</Label>
        <Input
          id="rb-port"
          type="number"
          min={1}
          max={65535}
          value={vars.port ?? 22}
          onChange={(e) => set('port', Number(e.target.value))}
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="rb-auth">{t('robots.form.authType')}</Label>
        <Select
          value={vars.authType}
          onValueChange={(v) => set('authType', v as CreateRobotVars['authType'])}
        >
          <SelectTrigger id="rb-auth">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="password">
              {t('robots.form.authType.password')}
            </SelectItem>
            <SelectItem value="privateKey">
              {t('robots.form.authType.privateKey')}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="rb-user">{t('robots.form.username')}</Label>
        <Input
          id="rb-user"
          required
          placeholder={t('robots.form.username.placeholder')}
          value={vars.username}
          onChange={(e) => set('username', e.target.value)}
        />
      </div>

      {vars.authType === 'password' ? (
        <div className="flex flex-col gap-1.5 md:col-span-2">
          <Label htmlFor="rb-pw">{t('robots.form.password')}</Label>
          <Input
            id="rb-pw"
            type="password"
            required
            value={vars.password ?? ''}
            onChange={(e) => set('password', e.target.value)}
          />
        </div>
      ) : (
        <div className="flex flex-col gap-1.5 md:col-span-2">
          <Label htmlFor="rb-pem">{t('robots.form.privateKeyPem')}</Label>
          <textarea
            id="rb-pem"
            required
            rows={5}
            placeholder={t('robots.form.privateKeyPem.placeholder')}
            value={vars.privateKeyPem ?? ''}
            onChange={(e) => set('privateKeyPem', e.target.value)}
            className="rounded-md border border-input bg-transparent px-3 py-2 font-mono text-xs shadow-xs"
          />
        </div>
      )}

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="rb-crit">{t('robots.form.criticality')}</Label>
        <Select
          value={vars.criticality ?? 'medium'}
          onValueChange={(v) =>
            set('criticality', v as CreateRobotVars['criticality'])
          }
        >
          <SelectTrigger id="rb-crit">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="low">{t('robots.form.criticality.low')}</SelectItem>
            <SelectItem value="medium">
              {t('robots.form.criticality.medium')}
            </SelectItem>
            <SelectItem value="high">{t('robots.form.criticality.high')}</SelectItem>
            <SelectItem value="critical">
              {t('robots.form.criticality.critical')}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="rb-tags">{t('robots.form.tags')}</Label>
        <Input
          id="rb-tags"
          placeholder={t('robots.form.tags.placeholder')}
          value={tagsRaw}
          onChange={(e) => setTagsRaw(e.target.value)}
        />
      </div>

      {create.isError && (
        <p className="md:col-span-2 text-sm text-destructive" role="alert">
          {create.error instanceof ApiError
            ? create.error.message
            : t('robots.form.error.fallback')}
        </p>
      )}
      {success && (
        <p className="md:col-span-2 text-sm text-emerald-600">{success}</p>
      )}

      <div className="md:col-span-2 flex justify-end">
        <Button
          type="submit"
          disabled={create.isPending || !isAdmin}
          title={!isAdmin ? t('common.role.required.admin') : undefined}
        >
          {create.isPending ? t('robots.form.submitting') : t('robots.form.submit')}
        </Button>
      </div>
    </form>
  )
}

export const Route = createFileRoute('/_authenticated/robots')({
  component: RobotsPage,
})
