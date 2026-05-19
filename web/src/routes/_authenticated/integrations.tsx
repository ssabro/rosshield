import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'

import { Webhook } from 'lucide-react'

import { ApiError } from '@/api/errors'
import {
  KNOWN_WEBHOOK_EVENTS,
  formatWebhookEvent,
  summarizeDeliveries,
  useCreateWebhook,
  useDeleteWebhook,
  useHasPermission,
  useTestWebhookEndpoint,
  useWebhookDeliveries,
  useWebhookEndpoints,
  webhookDeliveryStatus,
} from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { StatusBadge, type StatusKind } from '@/components/common/StatusBadge'
import { useT } from '@/i18n/t'
import { confirm } from '@/lib/confirm'
import { toast } from '@/lib/toast'
import { mutationGuardTitle, useIsOffline } from '@/lib/use-is-offline'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
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
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type {
  WebhookDelivery,
  WebhookDeliveryStatus,
  WebhookEndpoint,
  WebhookEventType,
  WebhookFormat,
} from '@/api/hooks'
import type { DictKey } from '@/i18n/dict'

// `/integrations` — Webhook endpoint CRUD + 최근 송출 조회 (B3).
//
// Backend HTTP 표면(E23-C)이 머지되기 전이므로 hooks.ts는 raw fetch로 작성됨.
// 본 페이지는 hooks 레이어가 정상 동작한다고 가정 — 401·로딩·에러 상태는 표준 처리.
//
// D-UI-1 Stage 4 — `window.confirm` / `window.alert` 제거 후 confirm()/toast 통합,
// delivery status는 StatusBadge로, CreateEndpointForm은 react-hook-form + zod pilot,
// 로딩은 TableRowSkeleton.
function IntegrationsPage(): React.ReactElement {
  const t = useT()
  const endpoints = useWebhookEndpoints()
  // RBAC Stage 5 — webhook은 tenant_admin.admin (§2.2 ID 3 — sso/webhook/users 통합).
  const isAdmin = useHasPermission('tenant_admin', 'admin')
  const isOffline = useIsOffline()
  const [showForm, setShowForm] = useState(false)
  const [selectedId, setSelectedId] = useState<string | null>(null)

  const selected = endpoints.data?.find((e) => e.id === selectedId) ?? null

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.integrations.title')}
        description={t('pages.integrations.description')}
        actions={
          <Button
            variant={showForm ? 'outline' : 'default'}
            size="sm"
            onClick={() => setShowForm((v) => !v)}
            disabled={!isAdmin || isOffline}
            title={mutationGuardTitle({
              isOffline,
              offlineLabel: t('pwa.offline.mutationBlocked'),
              fallback: !isAdmin ? t('common.role.required.admin') : undefined,
            })}
          >
            {showForm
              ? t('integrations.form.toggle.hide')
              : t('integrations.form.toggle.show')}
          </Button>
        }
      />

      {showForm && isAdmin && (
        <CreateEndpointForm
          onCreated={() => {
            setShowForm(false)
          }}
          isOffline={isOffline}
        />
      )}

      <EndpointsTable
        endpoints={endpoints.data ?? []}
        isPending={endpoints.isPending}
        isError={endpoints.isError}
        error={endpoints.error}
        selectedId={selectedId}
        onSelect={(id) => setSelectedId(id)}
        canMutate={isAdmin}
        isOffline={isOffline}
        canShowForm={!showForm}
        onRequestCreate={() => setShowForm(true)}
      />

      <DeliveriesSection
        endpoint={selected}
      />
    </div>
  )
}

// EndpointsTable — endpoint 목록 표.
function EndpointsTable({
  endpoints,
  isPending,
  isError,
  error,
  selectedId,
  onSelect,
  canMutate,
  isOffline,
  canShowForm,
  onRequestCreate,
}: {
  endpoints: WebhookEndpoint[]
  isPending: boolean
  isError: boolean
  error: unknown
  selectedId: string | null
  onSelect: (id: string) => void
  canMutate: boolean
  isOffline: boolean
  canShowForm: boolean
  onRequestCreate: () => void
}): React.ReactElement {
  const t = useT()
  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('integrations.table.name')}</TableHead>
            <TableHead>{t('integrations.table.url')}</TableHead>
            <TableHead>{t('integrations.table.events')}</TableHead>
            <TableHead>{t('integrations.table.format')}</TableHead>
            <TableHead>{t('integrations.table.enabled')}</TableHead>
            <TableHead>{t('integrations.table.created')}</TableHead>
            <TableHead className="text-right">
              {t('integrations.table.actions')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {isPending && (
            <TableRow>
              <TableCell colSpan={7} className="p-3">
                <TableRowSkeleton rows={3} columns={5} />
              </TableCell>
            </TableRow>
          )}
          {isError && (
            <TableRow>
              <TableCell colSpan={7} className="text-center text-destructive">
                {error instanceof ApiError
                  ? error.message
                  : t('integrations.error.fallback')}
              </TableCell>
            </TableRow>
          )}
          {!isPending && !isError && endpoints.length === 0 && (
            <TableRow>
              <TableCell colSpan={7} className="p-0">
                <EmptyState
                  icon={Webhook}
                  title={t('integrations.empty.title')}
                  description={t('integrations.empty.description')}
                  className="rounded-none border-0 bg-transparent"
                  action={
                    canMutate && canShowForm ? (
                      <Button
                        size="sm"
                        onClick={onRequestCreate}
                        disabled={isOffline}
                        title={
                          isOffline
                            ? t('pwa.offline.mutationBlocked')
                            : undefined
                        }
                      >
                        {t('integrations.empty.cta')}
                      </Button>
                    ) : undefined
                  }
                />
              </TableCell>
            </TableRow>
          )}
          {!isPending &&
            !isError &&
            endpoints.map((ep) => (
              <EndpointRow
                key={ep.id}
                endpoint={ep}
                selected={ep.id === selectedId}
                onSelect={() => onSelect(ep.id)}
                canMutate={canMutate}
                isOffline={isOffline}
              />
            ))}
        </TableBody>
      </Table>
    </div>
  )
}

function EndpointRow({
  endpoint,
  selected,
  onSelect,
  canMutate,
  isOffline,
}: {
  endpoint: WebhookEndpoint
  selected: boolean
  onSelect: () => void
  canMutate: boolean
  isOffline: boolean
}): React.ReactElement {
  const t = useT()
  const del = useDeleteWebhook()
  const name = endpointDisplayName(endpoint)
  const handleDelete = async (): Promise<void> => {
    const ok = await confirm({
      title: t('integrations.confirm.delete.title'),
      description: t('integrations.confirm.delete.description'),
      destructive: true,
      confirmText: t('integrations.confirm.delete.confirmText'),
      confirmLabel: t('integrations.confirm.delete.confirmLabel'),
    })
    if (!ok) return
    del.mutate(endpoint.id, {
      onSuccess: () => {
        toast.success(t('integrations.toast.delete.success'), {
          description: name,
        })
      },
      onError: (err) => {
        toast.error(t('integrations.toast.delete.error'), {
          description:
            err instanceof ApiError ? err.message : undefined,
        })
      },
    })
  }
  const events = endpoint.events ?? []
  const guardTitle = mutationGuardTitle({
    isOffline,
    offlineLabel: t('pwa.offline.mutationBlocked'),
    fallback: !canMutate ? t('common.role.required.admin') : undefined,
  })
  return (
    <TableRow data-selected={selected ? 'true' : undefined}>
      <TableCell className="font-medium">{name}</TableCell>
      <TableCell className="max-w-[20rem] truncate font-mono text-xs" title={endpoint.url}>
        {endpoint.url}
      </TableCell>
      <TableCell>
        <div className="flex flex-wrap gap-1">
          {events.length === 0 ? (
            <Badge variant="outline" className="text-[10px]">
              {t('integrations.table.events.all')}
            </Badge>
          ) : (
            events.map((e) => (
              <Badge key={e} variant="secondary" className="text-[10px]">
                {formatWebhookEvent(e)}
              </Badge>
            ))
          )}
        </div>
      </TableCell>
      <TableCell className="text-xs uppercase">{endpoint.format}</TableCell>
      <TableCell>
        <Badge
          variant={endpoint.enabled ? 'default' : 'outline'}
          className="text-[10px]"
        >
          {endpoint.enabled
            ? t('integrations.table.enabled.on')
            : t('integrations.table.enabled.off')}
        </Badge>
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {endpoint.createdAt
          ? new Date(endpoint.createdAt).toLocaleString()
          : '—'}
      </TableCell>
      <TableCell className="text-right">
        <div className="inline-flex flex-wrap justify-end gap-1">
          <Button
            size="sm"
            variant={selected ? 'default' : 'outline'}
            onClick={onSelect}
          >
            {t('integrations.action.select')}
          </Button>
          <TestButton
            endpointId={endpoint.id}
            endpointName={name}
            canMutate={canMutate}
            isOffline={isOffline}
          />
          <Button
            size="sm"
            variant="outline"
            onClick={handleDelete}
            disabled={del.isPending || !canMutate || isOffline}
            title={guardTitle}
          >
            {del.isPending
              ? t('integrations.action.deleting')
              : t('integrations.action.delete')}
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}

// TestButton — endpoint one-off ping (O7, E29 backend POST /webhooks/{id}/test).
function TestButton({
  endpointId,
  endpointName,
  canMutate,
  isOffline,
}: {
  endpointId: string
  endpointName: string
  canMutate: boolean
  isOffline: boolean
}): React.ReactElement {
  const t = useT()
  const test = useTestWebhookEndpoint()
  const handle = (e: React.MouseEvent): void => {
    e.stopPropagation()
    test.mutate(endpointId, {
      onSuccess: (res) => {
        if (res.success) {
          toast.success(t('integrations.toast.test.success.title'), {
            description:
              endpointName +
              ' · ' +
              t('integrations.toast.test.success.description', {
                status: String(res.status),
                latency: String(res.latencyMs),
              }),
          })
        } else {
          toast.error(t('integrations.toast.test.failure.title'), {
            description:
              endpointName +
              ' · ' +
              t('integrations.toast.test.failure.description', {
                status: String(res.status),
                error: res.error || t('common.error.unknown'),
              }),
          })
        }
      },
      onError: (err) => {
        toast.error(t('integrations.action.test.error.fallback'), {
          description:
            err instanceof ApiError ? err.message : undefined,
        })
      },
    })
  }
  return (
    <Button
      size="sm"
      variant="outline"
      onClick={handle}
      disabled={test.isPending || !canMutate || isOffline}
      title={
        isOffline
          ? t('pwa.offline.mutationBlocked')
          : !canMutate
            ? t('common.role.required.admin')
            : t('integrations.action.test.tooltip')
      }
    >
      {test.isPending
        ? t('integrations.action.test.sending')
        : t('integrations.action.test')}
    </Button>
  )
}

// DeliveriesSection — 선택된 endpoint의 최근 deliveries 표 + 통계 카드 (O7).
function DeliveriesSection({
  endpoint,
}: {
  endpoint: WebhookEndpoint | null
}): React.ReactElement {
  const t = useT()
  const deliveries = useWebhookDeliveries(endpoint?.id)

  const stats = summarizeDeliveries(deliveries.data ?? [])

  const title = endpoint
    ? t('integrations.deliveries.title', {
        name: endpointDisplayName(endpoint),
      })
    : t('integrations.deliveries.title.unselected')

  return (
    <section className="space-y-2">
      <h2 className="text-sm font-medium">{title}</h2>

      {endpoint && stats.total > 0 && (
        <div className="grid grid-cols-2 gap-2 rounded-md border bg-muted/30 p-3 text-xs sm:grid-cols-4">
          <StatCell label={t('integrations.deliveries.stats.success')} value={stats.success} variant="success" />
          <StatCell label={t('integrations.deliveries.stats.retrying')} value={stats.retrying} variant="warning" />
          <StatCell label={t('integrations.deliveries.stats.dead')} value={stats.dead} variant="destructive" />
          <StatCell label={t('integrations.deliveries.stats.pending')} value={stats.pending} variant="muted" />
        </div>
      )}

      {!endpoint ? (
        <p className="rounded-md border border-dashed border-border bg-muted/30 px-3 py-4 text-xs text-muted-foreground">
          {t('integrations.deliveries.unselected')}
        </p>
      ) : (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('integrations.deliveries.time')}</TableHead>
                <TableHead>{t('integrations.deliveries.event')}</TableHead>
                <TableHead>{t('integrations.deliveries.status')}</TableHead>
                <TableHead>{t('integrations.deliveries.attempt')}</TableHead>
                <TableHead>{t('integrations.deliveries.error')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {deliveries.isPending && (
                <TableRow>
                  <TableCell colSpan={5} className="p-3">
                    <TableRowSkeleton rows={3} columns={4} />
                  </TableCell>
                </TableRow>
              )}
              {deliveries.isError && (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className="text-center text-destructive"
                  >
                    {deliveries.error instanceof ApiError
                      ? deliveries.error.message
                      : t('integrations.deliveries.error.fallback')}
                  </TableCell>
                </TableRow>
              )}
              {!deliveries.isPending &&
                !deliveries.isError &&
                (deliveries.data ?? []).length === 0 && (
                  <TableRow>
                    <TableCell
                      colSpan={5}
                      className="text-center text-muted-foreground"
                    >
                      {t('integrations.deliveries.empty')}
                    </TableCell>
                  </TableRow>
                )}
              {!deliveries.isPending &&
                !deliveries.isError &&
                (deliveries.data ?? []).map((d) => (
                  <DeliveryRow key={d.id} delivery={d} />
                ))}
            </TableBody>
          </Table>
        </div>
      )}
    </section>
  )
}

function DeliveryRow({
  delivery,
}: {
  delivery: WebhookDelivery
}): React.ReactElement {
  const t = useT()
  const status = webhookDeliveryStatus(delivery)
  const time = delivery.lastAttemptedAt ?? delivery.createdAt
  return (
    <TableRow>
      <TableCell className="font-mono text-xs">
        {time ? new Date(time).toLocaleString() : '—'}
      </TableCell>
      <TableCell className="text-xs">
        {formatWebhookEvent(String(delivery.eventType))}
      </TableCell>
      <TableCell>
        <StatusBadge
          status={deliveryStatusKind(status)}
          label={t(deliveryStatusLabelKey(status))}
        />
      </TableCell>
      <TableCell className="font-mono text-xs">
        {delivery.attemptCount}
      </TableCell>
      <TableCell
        className="max-w-[28rem] truncate text-xs text-muted-foreground"
        title={delivery.lastError || String(delivery.lastResponseStatus)}
      >
        {delivery.lastError ||
          (delivery.lastResponseStatus
            ? `HTTP ${delivery.lastResponseStatus}`
            : '—')}
      </TableCell>
    </TableRow>
  )
}

// CreateEndpointForm — 신규 webhook endpoint 등록 폼 (react-hook-form + zod pilot).
//
// Stage 4 pilot — Form / FormField / FormMessage 패턴.
//   - zod schema로 field-level validation (URL · secret 길이).
//   - submit disabled state는 form.formState.isValid + isPending.
//   - 다중 체크박스(events) + select(format) + switch(enabled)는 setValue/watch로 결선.
function CreateEndpointForm({
  onCreated,
  isOffline,
}: {
  onCreated: () => void
  isOffline: boolean
}): React.ReactElement {
  const t = useT()
  const create = useCreateWebhook()

  const schema = z.object({
    name: z.string().optional(),
    url: z
      .string()
      .min(1, t('integrations.form.validation.url.required'))
      .url(t('integrations.form.validation.url.invalid')),
    secret: z.string().min(8, t('integrations.form.validation.secret.min')),
    events: z.array(z.string()),
    format: z.enum(['json', 'cef', 'ecs']),
    enabled: z.boolean(),
  })
  type FormValues = z.infer<typeof schema>

  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: '',
      url: '',
      secret: '',
      events: [],
      format: 'json',
      enabled: true,
    },
    mode: 'onBlur',
  })

  const events = (form.watch('events') ?? []) as WebhookEventType[]
  const format = form.watch('format') as WebhookFormat
  const enabled = form.watch('enabled') as boolean

  const toggleEvent = (e: WebhookEventType): void => {
    const next = events.includes(e)
      ? events.filter((x) => x !== e)
      : [...events, e]
    form.setValue('events', next, { shouldDirty: true, shouldValidate: true })
  }

  const onSubmit = (values: FormValues): void => {
    // 주의: backend WebhookEndpoint에 name 필드 미존재 — 본 stage는 form state로만 유지.
    // Backend 확장 시 hook + 본 mutate 인자에 name 추가 (dispatch 시점 메모).
    create.mutate(
      {
        url: values.url.trim(),
        secret: values.secret,
        events: values.events as WebhookEventType[],
        format: values.format,
        enabled: values.enabled,
      },
      {
        onSuccess: () => {
          toast.success(t('integrations.toast.create.success'), {
            description: values.url,
          })
          form.reset()
          onCreated()
        },
        onError: (err) => {
          toast.error(t('integrations.toast.create.error'), {
            description:
              err instanceof ApiError
                ? err.message
                : t('integrations.form.error.fallback'),
          })
        },
      },
    )
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className="grid grid-cols-1 gap-3 rounded-md border p-4 md:grid-cols-2"
        aria-label={t('integrations.form.section')}
      >
        <div className="md:col-span-2">
          <h3 className="text-sm font-medium">{t('integrations.form.section')}</h3>
        </div>

        <FormField
          control={form.control}
          name="name"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('integrations.form.name')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t('integrations.form.name.placeholder')}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormItem>
          <FormLabel htmlFor="wh-format">{t('integrations.form.format')}</FormLabel>
          <Select
            value={format}
            onValueChange={(v) =>
              form.setValue('format', v as 'json' | 'cef' | 'ecs', {
                shouldDirty: true,
              })
            }
          >
            <SelectTrigger id="wh-format">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="json">
                {t('integrations.form.format.json')}
              </SelectItem>
              <SelectItem value="cef">
                {t('integrations.form.format.cef')}
              </SelectItem>
              <SelectItem value="ecs">
                {t('integrations.form.format.ecs')}
              </SelectItem>
            </SelectContent>
          </Select>
        </FormItem>

        <FormField
          control={form.control}
          name="url"
          render={({ field }) => (
            <FormItem className="md:col-span-2">
              <FormLabel>{t('integrations.form.url')}</FormLabel>
              <FormControl>
                <Input
                  type="url"
                  placeholder={t('integrations.form.url.placeholder')}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="secret"
          render={({ field }) => (
            <FormItem className="md:col-span-2">
              <FormLabel>{t('integrations.form.secret')}</FormLabel>
              <FormControl>
                <Input
                  type="password"
                  placeholder={t('integrations.form.secret.placeholder')}
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t('integrations.form.secret.hint')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <fieldset className="md:col-span-2 flex flex-col gap-2">
          <legend className="text-sm font-medium">
            {t('integrations.form.events')}
          </legend>
          <p className="text-xs text-muted-foreground">
            {t('integrations.form.events.hint')}
          </p>
          <div className="flex flex-wrap gap-3">
            {KNOWN_WEBHOOK_EVENTS.map((e) => {
              const checked = events.includes(e)
              return (
                <label
                  key={e}
                  className="flex items-center gap-2 text-xs"
                >
                  <Checkbox
                    checked={checked}
                    onCheckedChange={() => toggleEvent(e)}
                    aria-label={t(eventLabelKey(e))}
                  />
                  <span className="font-mono">{t(eventLabelKey(e))}</span>
                </label>
              )
            })}
          </div>
        </fieldset>

        <div className="md:col-span-2 flex items-center gap-2">
          <Switch
            id="wh-enabled"
            checked={enabled}
            onCheckedChange={(v) =>
              form.setValue('enabled', Boolean(v), { shouldDirty: true })
            }
          />
          <Label htmlFor="wh-enabled" className="text-sm">
            {t('integrations.form.enabled')}
          </Label>
        </div>

        <div className="md:col-span-2 flex justify-end">
          <Button
            type="submit"
            disabled={create.isPending || isOffline || !form.formState.isValid}
            title={mutationGuardTitle({
              isOffline,
              offlineLabel: t('pwa.offline.mutationBlocked'),
            })}
          >
            {create.isPending
              ? t('integrations.form.submitting')
              : t('integrations.form.submit')}
          </Button>
        </div>
      </form>
    </Form>
  )
}

// endpointDisplayName — backend 모델에는 name 필드가 없음 (E23 한정).
//   "name"이 호스트에 보존돼 있으면 그것을, 없으면 URL host를 사용.
//   exported for unit testing.
export function endpointDisplayName(ep: WebhookEndpoint): string {
  const fromName = (ep as { name?: string }).name
  if (fromName && fromName.trim().length > 0) return fromName
  try {
    const u = new URL(ep.url)
    return u.host || ep.url
  } catch {
    return ep.url || ep.id
  }
}

// statusBadgeVariant — delivery status별 shadcn Badge variant 매핑.
//   StatusBadge 컴포넌트 도입(Stage 4) 이후 본 함수는 호환을 위해 export만 유지.
//   기존 단위 테스트 시그니처 보존.
export function statusBadgeVariant(
  status: WebhookDeliveryStatus,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status) {
    case 'success':
      return 'default'
    case 'dead':
      return 'destructive'
    case 'retrying':
      return 'secondary'
    default:
      return 'outline'
  }
}

// deliveryStatusKind — WebhookDeliveryStatus → StatusBadge StatusKind 매핑.
//   Stage 4 신규 export, 단위 테스트 대상.
export function deliveryStatusKind(
  status: WebhookDeliveryStatus,
): StatusKind {
  switch (status) {
    case 'success':
      return 'success'
    case 'dead':
      return 'failed'
    case 'retrying':
      return 'running'
    default:
      return 'pending'
  }
}

// deliveryStatusLabelKey — status별 dict 키 매핑.
export function deliveryStatusLabelKey(
  status: WebhookDeliveryStatus,
): DictKey {
  switch (status) {
    case 'success':
      return 'integrations.deliveries.status.success'
    case 'dead':
      return 'integrations.deliveries.status.dead'
    case 'retrying':
      return 'integrations.deliveries.status.retrying'
    default:
      return 'integrations.deliveries.status.pending'
  }
}

function eventLabelKey(e: WebhookEventType): DictKey {
  switch (e) {
    case 'scan.completed':
      return 'integrations.event.scan.completed'
    case 'insight.created':
      return 'integrations.event.insight.created'
    case 'audit.checkpoint':
      return 'integrations.event.audit.checkpoint'
  }
}

// StatCell — DeliveriesSection 통계 카드 셀 (O7).
//
// variant별 색상: success=primary, warning=amber, destructive=destructive, muted=muted.
type StatCellVariant = 'success' | 'warning' | 'destructive' | 'muted'

export function statCellColorClass(v: StatCellVariant): string {
  switch (v) {
    case 'success':
      return 'text-primary'
    case 'warning':
      return 'text-amber-600 dark:text-amber-400'
    case 'destructive':
      return 'text-destructive'
    default:
      return 'text-muted-foreground'
  }
}

function StatCell({
  label,
  value,
  variant,
}: {
  label: string
  value: number
  variant: StatCellVariant
}): React.ReactElement {
  return (
    <div className="space-y-0.5">
      <div className="text-[10px] uppercase text-muted-foreground">{label}</div>
      <div className={`text-base font-medium tabular-nums ${statCellColorClass(variant)}`}>
        {value}
      </div>
    </div>
  )
}

export const Route = createFileRoute('/_authenticated/integrations')({
  component: IntegrationsPage,
})
