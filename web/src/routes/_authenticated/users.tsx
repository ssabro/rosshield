import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { Users as UsersIcon } from 'lucide-react'

import { ApiError } from '@/api/errors'
import {
  useCreateInvitation,
  useDeleteInvitation,
  useInvitations,
  useIsAdmin,
} from '@/api/hooks'
import { EmptyState } from '@/components/layout/EmptyState'
import { PageHeader } from '@/components/layout/PageHeader'
import { useT } from '@/i18n/t'
import { requireRole } from '@/lib/route-guards'
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

import type {
  CreateInvitationResponse,
  InvitationView,
} from '@/api/hooks'
import type { DictKey } from '@/i18n/dict'

// `/users` — 사용자 초대 + 활성 초대 관리 (B2).
//
// admin 운영용 페이지. 인증 + admin role 가정. 백엔드는 admin 가드를 자체 처리하므로
// 본 페이지는 401/403을 친화 메시지로 표면화하기만 한다.
//
// 핵심 UX:
//   - 초대 생성 시 token이 1회 노출 → URL 형식으로 사용자에게 전달.
//   - 활성 초대 테이블에서 status 색상으로 한눈에 분류.
//   - 취소(revoke)는 즉시 만료 시킴. 사용된 token은 무효화.
function UsersPage(): React.ReactElement {
  const t = useT()
  const invitations = useInvitations()
  const isAdmin = useIsAdmin()
  const [created, setCreated] = useState<CreateInvitationResponse | null>(null)

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('pages.users.title')}
        description={t('pages.users.description')}
      />

      <CreateInvitationForm
        onCreated={(res) => setCreated(res)}
        canCreate={isAdmin}
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
        canRevoke={isAdmin}
      />
    </div>
  )
}

// CreateInvitationForm — email + roleName + expiresInHours 폼.
function CreateInvitationForm({
  onCreated,
  canCreate,
}: {
  onCreated: (res: CreateInvitationResponse) => void
  canCreate: boolean
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
        },
      },
    )
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="grid grid-cols-1 gap-3 rounded-md border p-4 md:grid-cols-4"
      aria-label={t('users.invite.section')}
    >
      <div className="md:col-span-4">
        <h3 className="text-sm font-medium">{t('users.invite.section')}</h3>
      </div>

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
          disabled={create.isPending || !canCreate}
          title={!canCreate ? t('common.role.required.admin') : undefined}
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
  canRevoke,
}: {
  invitations: InvitationView[]
  isPending: boolean
  isError: boolean
  error: unknown
  canRevoke: boolean
}): React.ReactElement {
  const t = useT()
  return (
    <div className="rounded-md border">
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
              <TableCell colSpan={8} className="text-center text-muted-foreground">
                {t('common.loading')}
              </TableCell>
            </TableRow>
          )}
          {isError && (
            <TableRow>
              <TableCell colSpan={8} className="text-center text-destructive">
                {error instanceof ApiError
                  ? error.message
                  : t('users.error.fallback')}
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
              />
            ))}
        </TableBody>
      </Table>
    </div>
  )
}

function InvitationRow({
  invitation,
  canRevoke,
}: {
  invitation: InvitationView
  canRevoke: boolean
}): React.ReactElement {
  const t = useT()
  const del = useDeleteInvitation()
  const status = invitationStatus(invitation)

  const handleDelete = (): void => {
    if (
      typeof window !== 'undefined' &&
      !window.confirm(t('users.action.delete.confirm'))
    ) {
      return
    }
    del.mutate(invitation.id)
  }

  const isTerminal = status === 'accepted' || status === 'expired'
  const adminTooltip = !canRevoke ? t('common.role.required.admin') : undefined

  return (
    <TableRow>
      <TableCell className="font-mono text-xs" title={invitation.id}>
        {shortId(invitation.id)}
      </TableCell>
      <TableCell className="text-sm">{invitation.email}</TableCell>
      <TableCell className="text-xs uppercase">{invitation.roleName}</TableCell>
      <TableCell>
        <Badge variant={statusBadgeVariant(status)} className="text-[10px]">
          {t(invitationStatusLabelKey(status))}
        </Badge>
      </TableCell>
      <TableCell
        className="font-mono text-xs text-muted-foreground"
        title={invitation.invitedBy}
      >
        {shortId(invitation.invitedBy)}
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
          onClick={handleDelete}
          disabled={del.isPending || isTerminal || !canRevoke}
          title={adminTooltip}
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

// statusBadgeVariant — invitation status별 Badge variant.
//   pending(default·primary 강조) / accepted(secondary·완료) / expired(outline·회색).
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

// shortId — ULID/UUID 같은 ID를 표시용으로 짧게 자른다.
function shortId(id: string): string {
  if (id.length <= 12) return id
  return `${id.slice(0, 8)}…${id.slice(-4)}`
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
  beforeLoad: () => requireRole('admin'),
  component: UsersPage,
})
