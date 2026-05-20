import { createFileRoute, useNavigate, useSearch } from '@tanstack/react-router'
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
  useWebhookDelivery,
  useWebhookEndpoints,
  webhookDeliveryStatus,
} from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { StatusBadge, type StatusKind } from '@/components/common/StatusBadge'
import { TruncatedId } from '@/components/common/TruncatedId'
import { useT } from '@/i18n/t'
import { confirm } from '@/lib/confirm'
import { toast } from '@/lib/toast'
import { undoableAction } from '@/lib/undoable'
import { mutationGuardTitle, useIsOffline } from '@/lib/use-is-offline'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
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
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/ui/tabs'

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
// a11y-drilldown.test.tsx mount용 named export.
export function IntegrationsPage(): React.ReactElement {
  const t = useT()
  const endpoints = useWebhookEndpoints()
  // RBAC Stage 5 — webhook은 tenant_admin.admin (§2.2 ID 3 — sso/webhook/users 통합).
  const isAdmin = useHasPermission('tenant_admin', 'admin')
  const isOffline = useIsOffline()
  const [createOpen, setCreateOpen] = useState(false)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  // D-UI-1 carryover — URL ?delivery=<id>로 delivery 상세 dialog deep link.
  const search = useSearch({ from: '/_authenticated/integrations' })
  const navigate = useNavigate()
  const selectedDeliveryId = search.delivery

  const selected = endpoints.data?.find((e) => e.id === selectedId) ?? null

  const openDelivery = (id: string): void => {
    void navigate({
      to: '/integrations',
      search: (prev) => ({ ...prev, delivery: id }),
      replace: true,
    })
  }
  const closeDelivery = (): void => {
    void navigate({
      to: '/integrations',
      search: (prev) => ({ ...prev, delivery: undefined }),
      replace: true,
    })
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.integrations.title')}
        description={t('pages.integrations.description')}
        actions={
          <Button
            size="sm"
            onClick={() => setCreateOpen(true)}
            disabled={!isAdmin || isOffline}
            title={mutationGuardTitle({
              isOffline,
              offlineLabel: t('pwa.offline.mutationBlocked'),
              fallback: !isAdmin ? t('common.role.required.admin') : undefined,
            })}
          >
            {t('integrations.form.toggle.show')}
          </Button>
        }
      />

      <EndpointsTable
        endpoints={endpoints.data ?? []}
        isPending={endpoints.isPending}
        isError={endpoints.isError}
        error={endpoints.error}
        selectedId={selectedId}
        onSelect={(id) => setSelectedId(id)}
        canMutate={isAdmin}
        isOffline={isOffline}
        canShowForm={isAdmin && !isOffline}
        onRequestCreate={() => setCreateOpen(true)}
      />

      <DeliveriesSection
        endpoint={selected}
        onSelectDelivery={openDelivery}
      />

      {isAdmin && (
        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogContent className="max-w-2xl">
            <DialogHeader>
              <DialogTitle>{t('integrations.form.section')}</DialogTitle>
              <DialogDescription>
                {t('integrations.form.dialog.description')}
              </DialogDescription>
            </DialogHeader>
            <CreateEndpointForm
              onCreated={() => setCreateOpen(false)}
              isOffline={isOffline}
            />
          </DialogContent>
        </Dialog>
      )}

      <DeliveryDetailDialog
        deliveryId={selectedDeliveryId}
        onClose={closeDelivery}
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
            <TableHead>{t('integrations.table.id')}</TableHead>
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
              <TableCell colSpan={8} className="p-3">
                <TableRowSkeleton rows={3} columns={5} />
              </TableCell>
            </TableRow>
          )}
          {isError && (
            <TableRow>
              <TableCell colSpan={8} className="text-center text-destructive">
                {error instanceof ApiError
                  ? error.message
                  : t('integrations.error.fallback')}
              </TableCell>
            </TableRow>
          )}
          {!isPending && !isError && endpoints.length === 0 && (
            <TableRow>
              <TableCell colSpan={8} className="p-0">
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
    // D-UI-1 P0 — Undo window: ConfirmDialog 후 5초 보류, undo 시 mutation 미실행.
    undoableAction({
      message: t('integrations.toast.delete.success'),
      description: name,
      undoLabel: t('common.undo'),
      action: () => del.mutateAsync(endpoint.id),
      errorLabel: t('integrations.toast.delete.error'),
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
      <TableCell>
        <TruncatedId id={endpoint.id} />
      </TableCell>
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
  onSelectDelivery,
}: {
  endpoint: WebhookEndpoint | null
  onSelectDelivery: (id: string) => void
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
                <TableHead>{t('integrations.deliveries.id')}</TableHead>
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
                  <TableCell colSpan={6} className="p-3">
                    <TableRowSkeleton rows={3} columns={4} />
                  </TableCell>
                </TableRow>
              )}
              {deliveries.isError && (
                <TableRow>
                  <TableCell
                    colSpan={6}
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
                      colSpan={6}
                      className="text-center text-muted-foreground"
                    >
                      {t('integrations.deliveries.empty')}
                    </TableCell>
                  </TableRow>
                )}
              {!deliveries.isPending &&
                !deliveries.isError &&
                (deliveries.data ?? []).map((d) => (
                  <DeliveryRow
                    key={d.id}
                    delivery={d}
                    onSelect={() => onSelectDelivery(d.id)}
                  />
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
  onSelect,
}: {
  delivery: WebhookDelivery
  onSelect: () => void
}): React.ReactElement {
  const t = useT()
  const status = webhookDeliveryStatus(delivery)
  const time = delivery.lastAttemptedAt ?? delivery.createdAt
  const handleKeyDown = (e: React.KeyboardEvent): void => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onSelect()
    }
  }
  return (
    <TableRow
      onClick={onSelect}
      onKeyDown={handleKeyDown}
      role="button"
      tabIndex={0}
      aria-label={t('integrations.delivery.row.aria')}
      className="cursor-pointer hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none"
    >
      <TableCell>
        <TruncatedId id={delivery.id} />
      </TableCell>
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

// DeliveryDetailDialog — 단건 delivery 상세 (3 tab: Request | Response | Retries).
//
// D-UI-1 carryover. cache fallback 패턴: backend 단건 endpoint 부재로 useWebhookDelivery
// hook이 list query cache에서 raw item을 가져옴. cache miss(reload·만료) 시 안내.
//
// 데이터 모델 한계:
//   - WebhookDelivery에 requestHeaders/responseBody/retryHistory raw 필드는 미존재.
//   - Request tab: eventType, eventId, payload(base64 decode JSON).
//   - Response tab: lastResponseStatus, lastError(짧은 메시지).
//   - Retries tab: attemptCount 기반 timeline (createdAt + lastAttemptedAt + nextAttemptAt).
function DeliveryDetailDialog({
  deliveryId,
  onClose,
}: {
  deliveryId: string | undefined
  onClose: () => void
}): React.ReactElement {
  const t = useT()
  const { data: delivery } = useWebhookDelivery(deliveryId)
  return (
    <Dialog
      open={Boolean(deliveryId)}
      onOpenChange={(next) => {
        if (!next) onClose()
      }}
    >
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t('integrations.delivery.detail.title')}</DialogTitle>
          <DialogDescription>
            {delivery
              ? `${formatWebhookEvent(String(delivery.eventType))} · ${delivery.eventId}`
              : (deliveryId ?? '')}
          </DialogDescription>
        </DialogHeader>
        {!delivery ? (
          <p className="rounded-md border border-dashed border-border bg-muted/30 px-3 py-4 text-xs text-muted-foreground">
            {t('integrations.delivery.notFound')}
          </p>
        ) : (
          <DeliveryDetailTabs delivery={delivery} />
        )}
      </DialogContent>
    </Dialog>
  )
}

function DeliveryDetailTabs({
  delivery,
}: {
  delivery: WebhookDelivery
}): React.ReactElement {
  const t = useT()
  return (
    <Tabs defaultValue="request" className="w-full">
      <TabsList>
        <TabsTrigger value="request">
          {t('integrations.delivery.tab.request')}
        </TabsTrigger>
        <TabsTrigger value="response">
          {t('integrations.delivery.tab.response')}
        </TabsTrigger>
        <TabsTrigger value="retries">
          {t('integrations.delivery.tab.retries')}
        </TabsTrigger>
      </TabsList>

      <TabsContent value="request" className="space-y-3">
        <div className="grid grid-cols-1 gap-2 text-xs sm:grid-cols-2">
          <div>
            <div className="text-muted-foreground">
              {t('integrations.delivery.eventType')}
            </div>
            <div className="font-mono">
              {formatWebhookEvent(String(delivery.eventType))}
            </div>
          </div>
          <div>
            <div className="text-muted-foreground">
              {t('integrations.delivery.eventId')}
            </div>
            <div className="font-mono break-all">{delivery.eventId}</div>
          </div>
        </div>
        <div>
          <h4 className="mb-1 text-sm font-medium">
            {t('integrations.delivery.headers')}
          </h4>
          <p className="text-[10px] text-muted-foreground">
            {t('integrations.delivery.headers.note')}
          </p>
        </div>
        <div>
          <h4 className="mb-1 text-sm font-medium">
            {t('integrations.delivery.payload')}
          </h4>
          <PayloadPreview base64={delivery.payloadBase64} />
        </div>
      </TabsContent>

      <TabsContent value="response" className="space-y-3">
        <p className="text-xs">
          <span className="text-muted-foreground">
            {t('integrations.delivery.status')}:{' '}
          </span>
          <span className="font-mono">
            {delivery.lastResponseStatus
              ? `HTTP ${delivery.lastResponseStatus}`
              : '—'}
          </span>
        </p>
        <pre className="max-h-96 overflow-auto rounded bg-muted p-3 text-xs whitespace-pre-wrap break-all">
          {delivery.lastError || t('integrations.delivery.no_response')}
        </pre>
      </TabsContent>

      <TabsContent value="retries" className="space-y-2">
        <p className="text-xs">
          <span className="text-muted-foreground">
            {t('integrations.delivery.attempt')}:{' '}
          </span>
          <span className="font-mono">{delivery.attemptCount}</span>
        </p>
        <ol className="space-y-2 border-l-2 border-border pl-3">
          <RetryTimelineItem
            label={t('integrations.delivery.firstAttempt')}
            timestamp={delivery.createdAt}
          />
          {delivery.lastAttemptedAt ? (
            <RetryTimelineItem
              label={t('integrations.delivery.lastAttempt')}
              timestamp={delivery.lastAttemptedAt}
              detail={
                delivery.lastResponseStatus
                  ? `HTTP ${delivery.lastResponseStatus}`
                  : undefined
              }
            />
          ) : null}
          {delivery.nextAttemptAt &&
          !delivery.succeeded &&
          delivery.attemptCount < 5 ? (
            <RetryTimelineItem
              label={t('integrations.delivery.nextAttempt')}
              timestamp={delivery.nextAttemptAt}
            />
          ) : null}
        </ol>
      </TabsContent>
    </Tabs>
  )
}

function RetryTimelineItem({
  label,
  timestamp,
  detail,
}: {
  label: string
  timestamp: string
  detail?: string
}): React.ReactElement {
  return (
    <li>
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className="font-mono text-xs">
        {timestamp ? new Date(timestamp).toLocaleString() : '—'}
      </div>
      {detail ? <div className="text-xs">{detail}</div> : null}
    </li>
  )
}

// PayloadPreview — base64-encoded payload를 JSON으로 디코드해 pretty-print.
//   디코딩 실패 시 raw base64를 잘림 표시(fallback).
//   exported for unit testing.
export function decodePayload(
  base64: string | undefined,
): { kind: 'json'; value: string } | { kind: 'raw'; value: string } | { kind: 'empty' } | { kind: 'error' } {
  if (!base64) return { kind: 'empty' }
  try {
    // atob: browser-native base64 decode. Node test 환경에서도 buffer polyfill로 동작.
    const raw =
      typeof atob === 'function'
        ? atob(base64)
        : Buffer.from(base64, 'base64').toString('utf-8')
    try {
      const parsed = JSON.parse(raw) as unknown
      return { kind: 'json', value: JSON.stringify(parsed, null, 2) }
    } catch {
      return { kind: 'raw', value: raw }
    }
  } catch {
    return { kind: 'error' }
  }
}

function PayloadPreview({
  base64,
}: {
  base64: string | undefined
}): React.ReactElement {
  const t = useT()
  const decoded = decodePayload(base64)
  if (decoded.kind === 'empty') {
    return (
      <p className="text-xs text-muted-foreground">
        {t('integrations.delivery.payload.empty')}
      </p>
    )
  }
  if (decoded.kind === 'error') {
    return (
      <p className="text-xs text-destructive">
        {t('integrations.delivery.payload.decodeError')}
      </p>
    )
  }
  return (
    <pre className="max-h-96 overflow-auto rounded bg-muted p-3 text-xs whitespace-pre-wrap break-all">
      {decoded.value}
    </pre>
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
        className="grid grid-cols-1 gap-3 md:grid-cols-2"
        aria-label={t('integrations.form.section')}
      >
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
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className={`text-base font-medium tabular-nums ${statCellColorClass(variant)}`}>
        {value}
      </div>
    </div>
  )
}

export const Route = createFileRoute('/_authenticated/integrations')({
  component: IntegrationsPage,
  validateSearch: (search: Record<string, unknown>): { delivery?: string } => {
    const d = typeof search.delivery === 'string' ? search.delivery : undefined
    return d ? { delivery: d } : {}
  },
})
