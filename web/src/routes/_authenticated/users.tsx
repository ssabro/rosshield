import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import {
  ShieldCheck,
  ShieldQuestion,
  Users as UsersIcon,
  UserCheck,
  UserCog,
  type LucideIcon,
} from 'lucide-react'

import { ApiError } from '@/api/errors'
import {
  useCreateInvitation,
  useDeleteInvitation,
  useHasPermission,
  useInvitations,
} from '@/api/hooks'
import { StatusBadge, type StatusKind } from '@/components/common/StatusBadge'
import { TruncatedId } from '@/components/common/TruncatedId'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { confirm } from '@/lib/confirm'
import { requirePermission } from '@/lib/route-guards'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'
import { mutationGuardTitle, useIsOffline } from '@/lib/use-is-offline'
import { Button } from '@/components/ui/button'
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type {
  CreateInvitationResponse,
  InvitationView,
} from '@/api/hooks'
import type { DictKey } from '@/i18n/dict'

// `/users` — 사용자 초대 + 활성 초대 관리 (B2).
//
// D-UI-1 Stage 4 — PageHeader 표준화 + StatusBadge / RoleBadge (a11y P0 보강) +
// Toast/ConfirmDialog/Skeleton/EmptyState 일관 패턴 적용. 기능·API·라우팅·RBAC
// 분기는 무변경.
//
// 핵심 UX:
//   - 초대 생성 시 token이 1회 노출 → URL 형식으로 사용자에게 전달 + toast 알림.
//   - role 컬럼은 색·아이콘·텍스트 3중 채널로 표시 (색만 의존 금지 — WCAG 1.4.1).
//   - 취소(revoke)는 비차단 dialog로 confirm + destructive 스타일 + toast 결과.
//
// D-UI-1 Stage 5b additional — axe scan mount용 named export.
export function UsersPage(): React.ReactElement {
  const t = useT()
  const invitations = useInvitations()
  // RBAC Stage 5 — invitation 관리는 tenant_admin.admin (§2.2 ID 1).
  const isAdmin = useHasPermission('tenant_admin', 'admin')
  const isOffline = useIsOffline()
  const [created, setCreated] = useState<CreateInvitationResponse | null>(null)
  const [inviteOpen, setInviteOpen] = useState(false)

  const totalCount = invitations.data?.length ?? 0

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.users.title')}
        description={t('pages.users.description')}
        badge={
          invitations.isSuccess ? (
            <span className="text-xs text-muted-foreground">
              {t('users.summary.total', { count: totalCount })}
            </span>
          ) : null
        }
        actions={
          <Button
            size="sm"
            onClick={() => setInviteOpen(true)}
            disabled={!isAdmin || isOffline}
            title={mutationGuardTitle({
              isOffline,
              offlineLabel: t('pwa.offline.mutationBlocked'),
              fallback: !isAdmin ? t('common.role.required.admin') : undefined,
            })}
          >
            {t('users.action.invite')}
          </Button>
        }
      />

      {created && (
        <CreatedTokenCard
          response={created}
          onDismiss={() => setCreated(null)}
        />
      )}

      <InvitationsTable
        invitations={invitations.data ?? []}
        isPending={invitations.isPending}
        isError={invitations.isError}
        error={invitations.error}
        onRetry={() => void invitations.refetch()}
        canRevoke={isAdmin}
        isOffline={isOffline}
        canInvite={isAdmin && !isOffline}
        onRequestInvite={() => setInviteOpen(true)}
      />

      {isAdmin && (
        <Dialog open={inviteOpen} onOpenChange={setInviteOpen}>
          <DialogContent className="max-w-xl">
            <DialogHeader>
              <DialogTitle>{t('users.invite.section')}</DialogTitle>
              <DialogDescription>
                {t('users.invite.dialog.description')}
              </DialogDescription>
            </DialogHeader>
            <CreateInvitationForm
              onCreated={(res) => {
                setCreated(res)
                setInviteOpen(false)
              }}
              canCreate={isAdmin}
              isOffline={isOffline}
            />
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}

// CreateInvitationForm — email + roleName + expiresInHours 폼.
function CreateInvitationForm({
  onCreated,
  canCreate,
  isOffline,
}: {
  onCreated: (res: CreateInvitationResponse) => void
  canCreate: boolean
  isOffline: boolean
}): React.ReactElement {
  const t = useT()
  const create = useCreateInvitation()
  const [email, setEmail] = useState('')
  const [roleName, setRoleName] = useState<string>('auditor')
  const [expiresInHours, setExpiresInHours] = useState<string>('')

  const handleSubmit = (e: React.FormEvent): void => {
    e.preventDefault()
    const ttl = expiresInHours.trim() === '' ? undefined : Number(expiresInHours)
    create.mutate(
      {
        email: email.trim(),
        roleName,
        expiresInHours: Number.isFinite(ttl) && (ttl ?? 0) > 0 ? ttl : undefined,
      },
      {
        onSuccess: (res) => {
          onCreated(res)
          setEmail('')
          setExpiresInHours('')
          toast.success(t('users.toast.created'), {
            description: t('users.toast.created.desc'),
          })
        },
        onError: (err) => {
          toast.error(t('users.toast.create.error'), {
            description: createInvitationErrorMessage(err, t),
          })
        },
      },
    )
  }

  return (
    <form
      id="invite-form"
      onSubmit={handleSubmit}
      className="grid grid-cols-1 gap-3 md:grid-cols-4"
      aria-label={t('users.invite.section')}
    >
      <div className="flex flex-col gap-1.5 md:col-span-2">
        <Label htmlFor="inv-email">{t('users.invite.email')}</Label>
        <Input
          id="inv-email"
          required
          type="email"
          placeholder={t('users.invite.email.placeholder')}
          value={email}
          onChange={(ev) => setEmail(ev.target.value)}
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="inv-role">{t('users.invite.role')}</Label>
        <Select value={roleName} onValueChange={setRoleName}>
          <SelectTrigger id="inv-role">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="admin">{t('users.invite.role.admin')}</SelectItem>
            <SelectItem value="auditor">{t('users.invite.role.auditor')}</SelectItem>
            <SelectItem value="operator">{t('users.invite.role.operator')}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="inv-expires">{t('users.invite.expires')}</Label>
        <Input
          id="inv-expires"
          type="number"
          min={1}
          placeholder={t('users.invite.expires.placeholder')}
          value={expiresInHours}
          onChange={(ev) => setExpiresInHours(ev.target.value)}
        />
      </div>

      <p className="md:col-span-4 text-xs text-muted-foreground">
        {t('users.invite.expires.hint')}
      </p>

      {create.isError && (
        <p className="md:col-span-4 text-sm text-destructive" role="alert">
          {createInvitationErrorMessage(create.error, t)}
        </p>
      )}

      <div className="md:col-span-4 flex justify-end">
        <Button
          type="submit"
          disabled={create.isPending || !canCreate || isOffline}
          title={mutationGuardTitle({
            isOffline,
            offlineLabel: t('pwa.offline.mutationBlocked'),
            fallback: !canCreate ? t('common.role.required.admin') : undefined,
          })}
        >
          {create.isPending
            ? t('users.invite.submitting')
            : t('users.invite.submit')}
        </Button>
      </div>
    </form>
  )
}

// CreatedTokenCard — 초대 생성 직후 token URL 노출 (1회).
function CreatedTokenCard({
  response,
  onDismiss,
}: {
  response: CreateInvitationResponse
  onDismiss: () => void
}): React.ReactElement {
  const t = useT()
  const [copied, setCopied] = useState(false)
  const origin =
    typeof window !== 'undefined' ? window.location.origin : ''
  const url = acceptUrl(response.token, origin)

  const handleCopy = async (): Promise<void> => {
    if (typeof navigator === 'undefined' || !navigator.clipboard) return
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      /* clipboard 미지원 환경 — 사용자가 직접 복사 */
    }
  }

  return (
    <div
      className="rounded-md border border-emerald-500/40 bg-emerald-500/5 p-4 space-y-2"
      role="status"
    >
      <div className="flex items-start justify-between gap-2">
        <p className="text-sm font-medium text-emerald-700 dark:text-emerald-400">
          {t('users.invite.success')}
        </p>
        <Button
          variant="ghost"
          size="sm"
          onClick={onDismiss}
          aria-label="dismiss"
        >
          ×
        </Button>
      </div>
      <Label htmlFor="inv-url" className="text-xs">
        {t('users.invite.token.label')}
      </Label>
      <div className="flex gap-2">
        <Input
          id="inv-url"
          readOnly
          value={url}
          className="font-mono text-xs"
        />
        <Button type="button" variant="outline" onClick={() => void handleCopy()}>
          {copied ? t('users.invite.token.copied') : t('users.invite.token.copy')}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        {t('users.invite.token.warning')}
      </p>
    </div>
  )
}

// InvitationsTable — 활성 초대 목록.
function InvitationsTable({
  invitations,
  isPending,
  isError,
  error,
  onRetry,
  canRevoke,
  isOffline,
  canInvite,
  onRequestInvite,
}: {
  invitations: InvitationView[]
  isPending: boolean
  isError: boolean
  error: unknown
  onRetry: () => void
  canRevoke: boolean
  isOffline: boolean
  canInvite: boolean
  onRequestInvite: () => void
}): React.ReactElement {
  const t = useT()
  return (
    <div className="rounded-md border">
      {/* 모바일 가로 스크롤 — table은 narrow viewport에서 줄바꿈 대신 overflow. */}
      <div className="w-full overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('users.table.id')}</TableHead>
              <TableHead>{t('users.table.email')}</TableHead>
              <TableHead>{t('users.table.role')}</TableHead>
              <TableHead>{t('users.table.status')}</TableHead>
              <TableHead>{t('users.table.invitedBy')}</TableHead>
              <TableHead>{t('users.table.created')}</TableHead>
              <TableHead>{t('users.table.expires')}</TableHead>
              <TableHead className="text-right">
                {t('users.table.actions')}
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
                <TableCell colSpan={8} className="p-0">
                  <EmptyState
                    variant="loading-fail"
                    title={t('users.error.fallback')}
                    description={
                      error instanceof ApiError
                        ? error.message
                        : undefined
                    }
                    action={
                      <Button size="sm" variant="outline" onClick={onRetry}>
                        {t('users.error.action.retry')}
                      </Button>
                    }
                    className="rounded-none border-0 bg-transparent"
                  />
                </TableCell>
              </TableRow>
            )}
            {!isPending && !isError && invitations.length === 0 && (
              <TableRow>
                <TableCell colSpan={8} className="p-0">
                  <EmptyState
                    icon={UsersIcon}
                    title={t('users.empty.title')}
                    description={t('users.empty.description')}
                    action={
                      canInvite ? (
                        <Button size="sm" onClick={onRequestInvite}>
                          {t('users.empty.cta')}
                        </Button>
                      ) : undefined
                    }
                    className="rounded-none border-0 bg-transparent"
                  />
                </TableCell>
              </TableRow>
            )}
            {!isPending &&
              !isError &&
              invitations.map((inv) => (
                <InvitationRow
                  key={inv.id}
                  invitation={inv}
                  canRevoke={canRevoke}
                  isOffline={isOffline}
                />
              ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function InvitationRow({
  invitation,
  canRevoke,
  isOffline,
}: {
  invitation: InvitationView
  canRevoke: boolean
  isOffline: boolean
}): React.ReactElement {
  const t = useT()
  const del = useDeleteInvitation()
  const status = invitationStatus(invitation)

  const handleDelete = async (): Promise<void> => {
    const ok = await confirm({
      title: t('users.action.delete.confirm.title'),
      description: t('users.action.delete.confirm.desc'),
      confirmLabel: t('users.action.delete.confirm.ok'),
      cancelLabel: t('users.action.delete.confirm.cancel'),
      destructive: true,
    })
    if (!ok) return
    del.mutate(invitation.id, {
      onSuccess: () => {
        toast.success(t('users.toast.deleted'))
      },
      onError: (err) => {
        toast.error(t('users.toast.delete.error'), {
          description: err instanceof ApiError ? err.message : undefined,
        })
      },
    })
  }

  const isTerminal = status === 'accepted' || status === 'expired'
  const guardTitle = mutationGuardTitle({
    isOffline,
    offlineLabel: t('pwa.offline.mutationBlocked'),
    fallback: !canRevoke ? t('common.role.required.admin') : undefined,
  })

  return (
    <TableRow>
      <TableCell>
        <TruncatedId id={invitation.id} />
      </TableCell>
      <TableCell className="text-sm">{invitation.email}</TableCell>
      <TableCell>
        <RoleBadge roleName={invitation.roleName} />
      </TableCell>
      <TableCell>
        <StatusBadge
          status={invitationStatusToBadgeKind(status)}
          label={t(invitationStatusLabelKey(status))}
        />
      </TableCell>
      <TableCell>
        <TruncatedId id={invitation.invitedBy} className="text-muted-foreground" />
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {invitation.createdAt
          ? new Date(invitation.createdAt).toLocaleString()
          : '—'}
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {invitation.expiresAt
          ? new Date(invitation.expiresAt).toLocaleString()
          : '—'}
      </TableCell>
      <TableCell className="text-right">
        <Button
          size="sm"
          variant="outline"
          onClick={() => void handleDelete()}
          disabled={del.isPending || isTerminal || !canRevoke || isOffline}
          title={guardTitle}
        >
          {del.isPending
            ? t('users.action.deleting')
            : t('users.action.delete')}
        </Button>
      </TableCell>
    </TableRow>
  )
}

// ────────────────────────────────────────────────────────────────────────
// RoleBadge — role을 색·아이콘·텍스트 3중 채널로 표시 (a11y review P0).
//
// a11y review에서 지적된 "role 컬럼이 단색 uppercase 텍스트 only" 이슈를 해소.
// admin은 운영 권한 강조 (sky), auditor 감사 강조 (emerald), operator (slate).
// ────────────────────────────────────────────────────────────────────────

interface RoleVisual {
  icon: LucideIcon
  className: string
  labelKey: DictKey
}

const ROLE_VISUAL: Record<string, RoleVisual> = {
  admin: {
    icon: ShieldCheck,
    className:
      'bg-sky-100 text-sky-900 dark:bg-sky-950 dark:text-sky-200 border-sky-300/40',
    labelKey: 'users.role.admin.label',
  },
  auditor: {
    icon: UserCheck,
    className:
      'bg-emerald-100 text-emerald-900 dark:bg-emerald-950 dark:text-emerald-200 border-emerald-300/40',
    labelKey: 'users.role.auditor.label',
  },
  operator: {
    icon: UserCog,
    className:
      'bg-slate-100 text-slate-700 dark:bg-slate-900 dark:text-slate-300 border-slate-300/40',
    labelKey: 'users.role.operator.label',
  },
}

const ROLE_UNKNOWN: RoleVisual = {
  icon: ShieldQuestion,
  className: 'bg-muted text-muted-foreground border-border',
  labelKey: 'users.role.unknown.label',
}

export function roleVisual(roleName: string): RoleVisual {
  return ROLE_VISUAL[roleName.toLowerCase()] ?? ROLE_UNKNOWN
}

function RoleBadge({ roleName }: { roleName: string }): React.ReactElement {
  const t = useT()
  const v = roleVisual(roleName)
  const Icon = v.icon
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded border px-2 py-0.5 text-xs font-medium',
        v.className,
      )}
      data-role={roleName.toLowerCase()}
      title={roleName}
    >
      <Icon className="size-3" aria-hidden />
      {t(v.labelKey)}
    </span>
  )
}

// ────────────────────────────────────────────────────────────────────────
// Helpers (export — 단위 테스트용)
// ────────────────────────────────────────────────────────────────────────

export type InvitationStatus = 'pending' | 'accepted' | 'expired'

// invitationStatus — invitation의 effective status를 분류.
//   - acceptedAt이 있으면 'accepted' (만료 무관, accepted가 우선).
//   - expiresAt이 현재 시각 이전이면 'expired'.
//   - 그 외 'pending'.
export function invitationStatus(
  inv: Pick<InvitationView, 'acceptedAt' | 'expiresAt'>,
): InvitationStatus {
  if (inv.acceptedAt) return 'accepted'
  if (inv.expiresAt) {
    const exp = new Date(inv.expiresAt).getTime()
    if (Number.isFinite(exp) && exp < Date.now()) {
      return 'expired'
    }
  }
  return 'pending'
}

// statusBadgeVariant — invitation status별 (legacy) shadcn Badge variant.
//   pending(default·primary 강조) / accepted(secondary·완료) / expired(outline·회색).
//
// D-UI-1 Stage 4 — 페이지는 StatusBadge로 전환되었으나 본 helper는 기존 테스트와
// 잠재 호출지 호환을 위해 export 유지. `invitationStatusToBadgeKind`가 신규 매핑.
export function statusBadgeVariant(
  status: InvitationStatus,
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status) {
    case 'pending':
      return 'default'
    case 'accepted':
      return 'secondary'
    case 'expired':
      return 'outline'
  }
}

// invitationStatusToBadgeKind — invitation status → StatusBadge `StatusKind`.
//   pending → 'pending' (gray clock), accepted → 'success' (green check),
//   expired → 'failed' (red x) — 만료는 사용자가 "받을 수 없는 상태" 임을 강조.
export function invitationStatusToBadgeKind(
  status: InvitationStatus,
): StatusKind {
  switch (status) {
    case 'pending':
      return 'pending'
    case 'accepted':
      return 'success'
    case 'expired':
      return 'failed'
  }
}

export function invitationStatusLabelKey(status: InvitationStatus): DictKey {
  switch (status) {
    case 'pending':
      return 'users.status.pending'
    case 'accepted':
      return 'users.status.accepted'
    case 'expired':
      return 'users.status.expired'
  }
}

// acceptUrl — origin + token으로 비인증 accept 페이지 URL 생성.
//   token은 URL-safe하지 않을 수 있으므로 encode. origin 끝의 /는 정리.
export function acceptUrl(token: string, origin: string): string {
  const cleanOrigin = origin.replace(/\/+$/, '')
  return `${cleanOrigin}/invitations/accept/${encodeURIComponent(token)}`
}

// createInvitationErrorMessage — 백엔드 에러를 친화 메시지로 매핑.
function createInvitationErrorMessage(
  err: unknown,
  t: (key: DictKey) => string,
): string {
  if (!(err instanceof ApiError)) {
    return t('users.invite.error.fallback')
  }
  if (err.status === 409) {
    // backend는 활성 초대 중복 + 사용자 이메일 중복 둘 다 409로 반환.
    // message에 "email" 포함 여부로 분기.
    const m = err.message.toLowerCase()
    if (m.includes('user') || m.includes('exist')) {
      return t('users.invite.error.email_exists')
    }
    return t('users.invite.error.duplicate')
  }
  return err.message || t('users.invite.error.fallback')
}

export const Route = createFileRoute('/_authenticated/users')({
  beforeLoad: () => requirePermission('tenant_admin', 'admin'),
  component: UsersPage,
})
