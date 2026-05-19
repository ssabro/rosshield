import { createFileRoute, Link } from '@tanstack/react-router'
import { Network } from 'lucide-react'
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
import { TruncatedId } from '@/components/common/TruncatedId'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { TableRowSkeleton } from '@/components/ui/skeleton'
import { useT } from '@/i18n/t'
import { confirm } from '@/lib/confirm'
import { requirePermission } from '@/lib/route-guards'
import { toast } from '@/lib/toast'
import { undoableAction } from '@/lib/undoable'

import type { Fleet, FleetPolicy } from '@/api/hooks'
import type { FormEvent } from 'react'

const LEVEL_VALUES = ['', 'L1', 'L2'] as const
const CRITICALITY_VALUES = ['', 'low', 'medium', 'high', 'critical'] as const

// `/fleets` — fleet 등록·이름 변경·삭제 페이지 (admin).
//
// D-UI-1 Stage 4 적용 패턴:
//   - PageHeader: actions에 "+ Fleet 생성" 버튼 (canCreate 시)
//   - Dialog: "+ Fleet 생성" 클릭 시 모달로 분리 (UI v0.6 pattern)
//   - TableRowSkeleton: list 로딩 시 layout shift 0
//   - EmptyState: 등록 0건 → "첫 fleet 생성" CTA → Dialog open
//   - TruncatedId: fleet.id를 짧게 + hover 시 원본 표시
//   - ConfirmDialog: delete typing confirmation (fleet 이름)
//   - toast: create/update/delete 성공·실패 비차단 통지
//
// RBAC Stage 5 — 매트릭스 매핑:
//   - 생성/삭제: tenant `fleet.admin` (§2.2 ID 14, 16)
//   - 단건 PATCH (FleetRow.canEdit): `fleet[X].fleet.write` (§2.2 ID 15)
function FleetsPage(): React.ReactElement {
  const t = useT()
  const canCreate = useHasPermission('fleet', 'admin')
  const fleetsQuery = useFleets()
  const [createOpen, setCreateOpen] = useState(false)

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('pages.fleets.title')}
        description={t('pages.fleets.description')}
        actions={
          canCreate && (
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              {t('fleets.action.create')}
            </Button>
          )
        }
      />

      <Card>
        <CardHeader>
          <CardTitle>{t('fleets.list.title')}</CardTitle>
        </CardHeader>
        <CardContent>
          {fleetsQuery.isPending ? (
            <TableRowSkeleton rows={4} columns={3} />
          ) : fleetsQuery.isError ? (
            <p className="text-sm text-destructive">
              {fleetsQuery.error instanceof Error
                ? fleetsQuery.error.message
                : t('fleets.list.error')}
            </p>
          ) : (fleetsQuery.data?.length ?? 0) === 0 ? (
            <EmptyState
              icon={Network}
              title={t('fleets.list.empty')}
              action={
                canCreate ? (
                  <Button size="sm" onClick={() => setCreateOpen(true)}>
                    {t('fleets.empty.cta')}
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <div className="space-y-2">
              {fleetsQuery.data?.map((f) => (
                <FleetRow key={f.id} fleet={f} />
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {canCreate && (
        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogContent className="max-w-xl">
            <DialogHeader>
              <DialogTitle>{t('fleets.form.title')}</DialogTitle>
              <DialogDescription>
                {t('fleets.form.dialog.description')}
              </DialogDescription>
            </DialogHeader>
            <CreateFleetForm onCreated={() => setCreateOpen(false)} />
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}

// CreateFleetForm — Dialog body 안의 fleet 생성 form.
//   Card·Header·Title은 상위 Dialog가 담당하므로 form만 렌더.
function CreateFleetForm({
  onCreated,
}: {
  onCreated: () => void
}): React.ReactElement {
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
          toast.success(t('fleets.form.toast.success'))
          setName('')
          setDescription('')
          setPolicy({})
          onCreated()
        },
        onError: (err) => {
          const msg =
            err instanceof ApiError && err.status === 409
              ? t('fleets.form.error.duplicate')
              : err instanceof Error
                ? err.message
                : t('fleets.form.error.fallback')
          setError(msg)
          toast.error(msg)
        },
      },
    )
  }

  return (
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
      <div className="flex justify-end">
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? t('fleets.form.submitting') : t('fleets.form.submit')}
        </Button>
      </div>
    </form>
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
        <div className="flex flex-wrap items-center gap-2">
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
          <TruncatedId id={fleet.id} className="text-muted-foreground" />
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
        onSuccess: () => {
          toast.success(t('fleets.row.update.toast.success'))
          onDone()
        },
        onError: (err) => {
          const msg =
            err instanceof ApiError && err.status === 409
              ? t('fleets.form.error.duplicate')
              : err instanceof Error
                ? err.message
                : t('fleets.form.error.fallback')
          setError(msg)
          toast.error(msg)
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

// DeleteFleetButton — typing confirmation (fleet 이름) + toast 성공/실패 통지.
//
// D-UI-1 Stage 4 — inline 2-step → imperative confirm() Promise로 a11y(focus
// trap, ESC)와 실수 차단 강화.
function DeleteFleetButton({ fleet }: { fleet: Fleet }): React.ReactElement {
  const t = useT()
  const del = useDeleteFleet()

  const handleClick = async (): Promise<void> => {
    const ok = await confirm({
      title: t('fleets.row.delete.confirm.title'),
      description: `${t('fleets.row.delete.confirm.description')}\n\n${t('fleets.row.delete.confirm.typingHint')}`,
      confirmText: fleet.name,
      confirmLabel: t('fleets.row.delete.yes'),
      cancelLabel: t('fleets.row.cancel'),
      destructive: true,
    })
    if (!ok) return
    // D-UI-1 P0 — Undo window: ConfirmDialog 후 5초 보류, undo 시 mutation 미실행.
    undoableAction({
      message: t('fleets.row.delete.toast.success'),
      undoLabel: t('common.undo'),
      action: () => del.mutateAsync(fleet.id),
      errorLabel: t('fleets.row.delete.error'),
    })
  }

  return (
    <Button
      variant="destructive"
      size="sm"
      disabled={del.isPending}
      onClick={() => {
        void handleClick()
      }}
    >
      {del.isPending ? t('fleets.form.submitting') : t('fleets.row.delete')}
    </Button>
  )
}

export const Route = createFileRoute('/_authenticated/fleets')({
  component: FleetsPage,
  // RBAC Stage 5 — sidebar `system.read` 매핑과 일관 (admin/auditor 묶음).
  beforeLoad: () => requirePermission('system', 'read'),
})
