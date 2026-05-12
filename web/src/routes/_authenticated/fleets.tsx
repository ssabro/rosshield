import { createFileRoute, Link } from '@tanstack/react-router'
import { useState } from 'react'

import { ApiError } from '@/api/errors'
import {
  useCreateFleet,
  useDeleteFleet,
  useFleets,
  useIsAdmin,
  useUpdateFleet,
} from '@/api/hooks'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { requireRole } from '@/lib/route-guards'
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

import type { Fleet } from '@/api/hooks'
import type { FormEvent } from 'react'

// `/fleets` — fleet 등록·이름 변경·삭제 페이지 (admin).
function FleetsPage(): React.ReactElement {
  const t = useT()
  const isAdmin = useIsAdmin()
  const fleetsQuery = useFleets()

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('pages.fleets.title')}
        description={t('pages.fleets.description')}
      />

      {isAdmin && <CreateFleetCard />}

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
                <FleetRow key={f.id} fleet={f} canEdit={isAdmin} />
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
  const [error, setError] = useState('')
  const create = useCreateFleet()

  const handleSubmit = (e: FormEvent<HTMLFormElement>): void => {
    e.preventDefault()
    setError('')
    create.mutate(
      { name, description },
      {
        onSuccess: () => {
          setName('')
          setDescription('')
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

function FleetRow({
  fleet,
  canEdit,
}: {
  fleet: Fleet
  canEdit: boolean
}): React.ReactElement {
  const t = useT()
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
  const [error, setError] = useState('')
  const update = useUpdateFleet()

  const handleSubmit = (e: FormEvent<HTMLFormElement>): void => {
    e.preventDefault()
    setError('')
    update.mutate(
      { fleetId: fleet.id, name, description },
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
      className="space-y-2 rounded border border-primary px-3 py-2 text-sm"
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
  beforeLoad: () => requireRole('admin', 'auditor'),
})
