import { createFileRoute, Link } from '@tanstack/react-router'
import { useState } from 'react'

import { ApiError } from '@/api/errors'
import {
  useCreateFleet,
  useDeleteFleet,
  useFleets,
  useHasPermission,
  usePacks,
  useUpdateFleet,
} from '@/api/hooks'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { requirePermission } from '@/lib/route-guards'
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

import type { Fleet, FleetPolicy } from '@/api/hooks'
import type { FormEvent } from 'react'

const LEVEL_VALUES = ['', 'L1', 'L2'] as const
const CRITICALITY_VALUES = ['', 'low', 'medium', 'high', 'critical'] as const

// `/fleets` — fleet 등록·이름 변경·삭제 페이지 (admin).
//
// RBAC Stage 5 — 매트릭스 매핑:
//   - 생성/삭제: tenant `fleet.admin` (§2.2 ID 14, 16)
//   - 단건 PATCH (FleetRow.canEdit): `fleet[X].fleet.write` (§2.2 ID 15)
function FleetsPage(): React.ReactElement {
  const t = useT()
  const canCreate = useHasPermission('fleet', 'admin')
  const fleetsQuery = useFleets()

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('pages.fleets.title')}
        description={t('pages.fleets.description')}
      />

      {canCreate && <CreateFleetCard />}

      <Card>
        <CardHeader>
          <CardTitle>{t('fleets.list.title')}</CardTitle>
        </CardHeader>
        <CardContent>
          {fleetsQuery.isPending ? (
            <p className="text-sm text-muted-foreground">
              {t('fleets.list.loading')}
            </p>
          ) : fleetsQuery.isError ? (
            <p className="text-sm text-destructive">
              {fleetsQuery.error instanceof Error
                ? fleetsQuery.error.message
                : t('fleets.list.error')}
            </p>
          ) : (fleetsQuery.data?.length ?? 0) === 0 ? (
            <p className="text-sm text-muted-foreground">
              {t('fleets.list.empty')}
            </p>
          ) : (
            <div className="space-y-2">
              {fleetsQuery.data?.map((f) => (
                <FleetRow key={f.id} fleet={f} />
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function CreateFleetCard(): React.ReactElement {
  const t = useT()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [policy, setPolicy] = useState<FleetPolicy>({})
  const [error, setError] = useState('')
  const create = useCreateFleet()

  const handleSubmit = (e: FormEvent<HTMLFormElement>): void => {
    e.preventDefault()
    setError('')
    create.mutate(
      { name, description, policy: hasPolicyContent(policy) ? policy : undefined },
      {
        onSuccess: () => {
          setName('')
          setDescription('')
          setPolicy({})
        },
        onError: (err) => {
          if (err instanceof ApiError && err.status === 409) {
            setError(t('fleets.form.error.duplicate'))
          } else {
            setError(err instanceof Error ? err.message : t('fleets.form.error.fallback'))
          }
        },
      },
    )
  }

  return (
    <Card className="max-w-xl">
      <CardHeader>
        <CardTitle>{t('fleets.form.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-3">
          <div className="space-y-2">
            <Label htmlFor="fleet-name">{t('fleets.form.name')}</Label>
            <Input
              id="fleet-name"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('fleets.form.name.placeholder')}
              maxLength={200}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="fleet-description">{t('fleets.form.description')}</Label>
            <Input
              id="fleet-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t('fleets.form.description.placeholder')}
              maxLength={500}
            />
          </div>
          <PolicyFormFields policy={policy} onChange={setPolicy} idPrefix="create" />
          {error && (
            <p className="text-sm text-destructive" role="alert">
              {error}
            </p>
          )}
          <Button type="submit" disabled={create.isPending}>
            {create.isPending ? t('fleets.form.submitting') : t('fleets.form.submit')}
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}

// hasPolicyContent — 모든 필드 빈 정책이면 false (서버에 nil 전달, default 적용).
function hasPolicyContent(p: FleetPolicy): boolean {
  return !!(p.defaultBaselineId || p.defaultLevel || p.defaultCriticality || p.scanSchedule)
}

// PolicyFormFields는 Create/Edit form 양쪽에서 공유하는 4 필드 정책 입력입니다.
function PolicyFormFields({
  policy,
  onChange,
  idPrefix,
}: {
  policy: FleetPolicy
  onChange: (p: FleetPolicy) => void
  idPrefix: string
}): React.ReactElement {
  const t = useT()
  const packsQuery = usePacks()
  const baselineId = `${idPrefix}-policy-baseline`
  const levelId = `${idPrefix}-policy-level`
  const criticalityId = `${idPrefix}-policy-criticality`
  const scheduleId = `${idPrefix}-policy-schedule`

  return (
    <div className="space-y-3 rounded border border-border p-3">
      <p className="text-xs font-medium text-muted-foreground">
        {t('fleets.form.policy.title')}
      </p>
      <div className="space-y-2">
        <Label htmlFor={baselineId}>{t('fleets.form.policy.baseline')}</Label>
        {packsQuery.isPending || packsQuery.isError ? (
          <Input
            id={baselineId}
            value={policy.defaultBaselineId ?? ''}
            onChange={(e) =>
              onChange({ ...policy, defaultBaselineId: e.target.value })
            }
            placeholder={t('fleets.form.policy.baseline.placeholder')}
          />
        ) : (
          <Select
            value={policy.defaultBaselineId ?? '__none__'}
            onValueChange={(v) =>
              onChange({ ...policy, defaultBaselineId: v === '__none__' ? '' : v })
            }
          >
            <SelectTrigger id={baselineId}>
              <SelectValue placeholder={t('fleets.form.policy.baseline.placeholder')} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__none__">{t('fleets.form.policy.none')}</SelectItem>
              {packsQuery.data?.map((p) => (
                <SelectItem key={p.id} value={p.packKey ?? p.id}>
                  {p.name} ({p.version})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>
      <div className="grid grid-cols-2 gap-2">
        <div className="space-y-2">
          <Label htmlFor={levelId}>{t('fleets.form.policy.level')}</Label>
          <Select
            value={policy.defaultLevel || '__none__'}
            onValueChange={(v) =>
              onChange({
                ...policy,
                defaultLevel: (v === '__none__' ? '' : v) as FleetPolicy['defaultLevel'],
              })
            }
          >
            <SelectTrigger id={levelId}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__none__">{t('fleets.form.policy.none')}</SelectItem>
              {LEVEL_VALUES.filter((v) => v !== '').map((v) => (
                <SelectItem key={v} value={v}>
                  {v}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label htmlFor={criticalityId}>{t('fleets.form.policy.criticality')}</Label>
          <Select
            value={policy.defaultCriticality || '__none__'}
            onValueChange={(v) =>
              onChange({
                ...policy,
                defaultCriticality: (v === '__none__'
                  ? ''
                  : v) as FleetPolicy['defaultCriticality'],
              })
            }
          >
            <SelectTrigger id={criticalityId}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__none__">{t('fleets.form.policy.none')}</SelectItem>
              {CRITICALITY_VALUES.filter((v) => v !== '').map((v) => (
                <SelectItem key={v} value={v}>
                  {v}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className="space-y-2">
        <Label htmlFor={scheduleId}>{t('fleets.form.policy.schedule')}</Label>
        <Input
          id={scheduleId}
          value={policy.scanSchedule ?? ''}
          onChange={(e) => onChange({ ...policy, scanSchedule: e.target.value })}
          placeholder={t('fleets.form.policy.schedule.placeholder')}
        />
      </div>
    </div>
  )
}

function FleetRow({
  fleet,
}: {
  fleet: Fleet
}): React.ReactElement {
  const t = useT()
  // RBAC Stage 5 — fleet 단건 PATCH는 fleet[X].fleet.write (§2.2 ID 15).
  //   admin tenant scope는 무관 통과 — fleet-admin은 fleet 일치 시만.
  const canEdit = useHasPermission('fleet', 'write', fleet.id)
  const [editing, setEditing] = useState(false)
  if (editing) {
    return (
      <EditFleetForm
        fleet={fleet}
        onCancel={() => setEditing(false)}
        onDone={() => setEditing(false)}
      />
    )
  }
  return (
    <div className="flex items-center justify-between rounded border border-border px-3 py-2 text-sm">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <Link
            to="/fleets/$fleetId"
            params={{ fleetId: fleet.id }}
            className="font-medium hover:underline"
          >
            {fleet.name}
          </Link>
          <Badge variant="secondary" className="text-xs">
            {t('fleets.row.robotCount', { count: fleet.robotCount })}
          </Badge>
          <span className="font-mono text-xs text-muted-foreground">{fleet.id}</span>
        </div>
        {fleet.description && (
          <p className="mt-0.5 text-xs text-muted-foreground">{fleet.description}</p>
        )}
      </div>
      {canEdit && (
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
            {t('fleets.row.edit')}
          </Button>
          <DeleteFleetButton fleet={fleet} />
        </div>
      )}
    </div>
  )
}

function EditFleetForm({
  fleet,
  onCancel,
  onDone,
}: {
  fleet: Fleet
  onCancel: () => void
  onDone: () => void
}): React.ReactElement {
  const t = useT()
  const [name, setName] = useState(fleet.name)
  const [description, setDescription] = useState(fleet.description ?? '')
  const [policy, setPolicy] = useState<FleetPolicy>({ ...fleet.policy })
  const [error, setError] = useState('')
  const update = useUpdateFleet()

  const handleSubmit = (e: FormEvent<HTMLFormElement>): void => {
    e.preventDefault()
    setError('')
    update.mutate(
      { fleetId: fleet.id, name, description, policy },
      {
        onSuccess: () => onDone(),
        onError: (err) => {
          if (err instanceof ApiError && err.status === 409) {
            setError(t('fleets.form.error.duplicate'))
          } else {
            setError(err instanceof Error ? err.message : t('fleets.form.error.fallback'))
          }
        },
      },
    )
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="space-y-3 rounded border border-primary px-3 py-2 text-sm"
    >
      <Input
        required
        value={name}
        onChange={(e) => setName(e.target.value)}
        maxLength={200}
        placeholder={t('fleets.form.name.placeholder')}
      />
      <Input
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        maxLength={500}
        placeholder={t('fleets.form.description.placeholder')}
      />
      <PolicyFormFields policy={policy} onChange={setPolicy} idPrefix={`edit-${fleet.id}`} />
      {error && (
        <p className="text-xs text-destructive" role="alert">
          {error}
        </p>
      )}
      <div className="flex items-center gap-2">
        <Button type="submit" size="sm" disabled={update.isPending}>
          {update.isPending ? t('fleets.form.submitting') : t('fleets.row.save')}
        </Button>
        <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
          {t('fleets.row.cancel')}
        </Button>
      </div>
    </form>
  )
}

function DeleteFleetButton({ fleet }: { fleet: Fleet }): React.ReactElement {
  const t = useT()
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState('')
  const del = useDeleteFleet()

  if (confirming) {
    return (
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted-foreground">{t('fleets.row.confirm')}</span>
        <Button
          variant="destructive"
          size="sm"
          onClick={() =>
            del.mutate(fleet.id, {
              onError: (e) =>
                setError(e instanceof Error ? e.message : t('fleets.row.delete.error')),
            })
          }
          disabled={del.isPending}
        >
          {del.isPending ? t('fleets.form.submitting') : t('fleets.row.delete.yes')}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            setConfirming(false)
            setError('')
          }}
        >
          {t('fleets.row.cancel')}
        </Button>
        {error && <span className="text-xs text-destructive">{error}</span>}
      </div>
    )
  }
  return (
    <Button variant="destructive" size="sm" onClick={() => setConfirming(true)}>
      {t('fleets.row.delete')}
    </Button>
  )
}

export const Route = createFileRoute('/_authenticated/fleets')({
  component: FleetsPage,
  // RBAC Stage 5 — sidebar `system.read` 매핑과 일관 (admin/auditor 묶음).
  beforeLoad: () => requirePermission('system', 'read'),
})
