import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { CheckCircle2, ShieldCheck } from 'lucide-react'
import { useEffect, useState } from 'react'

import { ApiError } from '@/api/errors'
import {
  useAcceptInvitation,
  useInvitationByToken,
} from '@/api/hooks'
import { useT } from '@/i18n/t'
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

import type { InvitationPreview } from '@/api/hooks'
import type { DictKey } from '@/i18n/dict'

// `/invitations/accept/{token}` — 비인증 invitation 수락 페이지 (B2).
//
// 자체 풀스크린 레이아웃(Sidebar/Header 없음 — _authenticated 밖).
// mount 시 GET by-token으로 미리보기 로드 → 상태별 분기:
//   - active: 폼(email, displayName, password) → POST accept → /login navigate.
//   - expired/used: 안내 박스 + /login 링크.
//   - notfound: 404 안내.
//
// AcceptInvitationPage — route component. Route param에서 token 추출 후 View로 위임.
function AcceptInvitationPage(): React.ReactElement {
  const { token } = Route.useParams()
  return <AcceptInvitationView token={token} />
}

// AcceptInvitationView — token을 prop으로 받는 inner view.
//   D-UI-1 Stage 5b additional — axe scan mount용 named export. 라우터/route param
//   의존이 없어 jsdom에서 그대로 mount 가능.
export function AcceptInvitationView({ token }: { token: string }): React.ReactElement {
  const t = useT()
  const preview = useInvitationByToken(token)

  return (
    <div className="flex min-h-screen items-center justify-center bg-gradient-to-br from-muted via-background to-muted px-4">
      <div className="w-full max-w-md space-y-6">
        <div className="flex flex-col items-center gap-3 text-center">
          <div className="rounded-xl bg-primary/10 p-3">
            <ShieldCheck className="h-8 w-8 text-primary" aria-hidden />
          </div>
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">
              {t('invitations.accept.title')}
            </h1>
            <p className="text-sm text-muted-foreground">
              {t('invitations.accept.description')}
            </p>
          </div>
        </div>

        {preview.isPending && (
          <Card>
            <CardContent className="py-10 text-center text-sm text-muted-foreground">
              {t('invitations.accept.loading')}
            </CardContent>
          </Card>
        )}

        {preview.isError && (
          <NotFoundCard error={preview.error} />
        )}

        {!preview.isPending && !preview.isError && preview.data && (
          <PreviewBranch token={token} preview={preview.data} />
        )}
      </div>
    </div>
  )
}

// PreviewBranch — preview 데이터에 따라 상태별 카드를 렌더.
function PreviewBranch({
  token,
  preview,
}: {
  token: string
  preview: InvitationPreview
}): React.ReactElement {
  const state = invitationPreviewState(preview)
  if (state === 'used') {
    return <UsedCard />
  }
  if (state === 'expired') {
    return <ExpiredCard />
  }
  return <AcceptForm token={token} preview={preview} />
}

// AcceptForm — 활성 초대에 대한 수락 폼.
function AcceptForm({
  token,
  preview,
}: {
  token: string
  preview: InvitationPreview
}): React.ReactElement {
  const t = useT()
  const navigate = useNavigate()
  const accept = useAcceptInvitation()

  const [email, setEmail] = useState(preview.email)
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const [validationErrors, setValidationErrors] = useState<string[]>([])
  const [success, setSuccess] = useState(false)

  // 초대된 이메일이 변경되면 reset.
  useEffect(() => {
    setEmail(preview.email)
  }, [preview.email])

  const handleSubmit = (e: React.FormEvent): void => {
    e.preventDefault()
    const errs = validateAcceptForm({ email, displayName, password })
    setValidationErrors(errs)
    if (errs.length > 0) return

    accept.mutate(
      {
        token,
        email: email.trim(),
        displayName: displayName.trim(),
        password,
      },
      {
        onSuccess: () => {
          setSuccess(true)
          // 잠깐 메시지 표시 후 login으로 이동.
          setTimeout(() => {
            void navigate({ to: '/login' })
          }, 1500)
        },
      },
    )
  }

  if (success) {
    return (
      <Card>
        <CardHeader className="items-center text-center">
          <CheckCircle2
            className="h-10 w-10 text-emerald-500"
            aria-hidden
          />
          <CardTitle>{t('invitations.accept.success')}</CardTitle>
        </CardHeader>
        <CardContent className="text-center">
          <Button
            type="button"
            onClick={() => void navigate({ to: '/login' })}
          >
            {t('invitations.accept.toLogin')}
          </Button>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('invitations.accept.title')}</CardTitle>
        <CardDescription>
          <PreviewSummary preview={preview} />
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="acc-email">{t('invitations.accept.form.email')}</Label>
            <Input
              id="acc-email"
              type="email"
              required
              readOnly
              value={email}
              onChange={(ev) => setEmail(ev.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              {t('invitations.accept.form.email.hint')}
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="acc-name">
              {t('invitations.accept.form.displayName')}
            </Label>
            <Input
              id="acc-name"
              required
              autoFocus
              placeholder={t('invitations.accept.form.displayName.placeholder')}
              value={displayName}
              onChange={(ev) => setDisplayName(ev.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="acc-password">
              {t('invitations.accept.form.password')}
            </Label>
            <Input
              id="acc-password"
              type="password"
              required
              autoComplete="new-password"
              minLength={12}
              placeholder={t('invitations.accept.form.password.placeholder')}
              value={password}
              onChange={(ev) => setPassword(ev.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              {t('invitations.accept.form.password.hint')}
            </p>
          </div>

          {validationErrors.length > 0 && (
            <ul
              className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm text-destructive space-y-1"
              role="alert"
            >
              {validationErrors.map((err) => (
                <li key={err}>{t(validationErrorKey(err))}</li>
              ))}
            </ul>
          )}

          {accept.isError && (
            <p
              className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm text-destructive"
              role="alert"
            >
              {accept.error instanceof ApiError
                ? accept.error.message
                : t('invitations.accept.error.fallback')}
            </p>
          )}

          <Button
            type="submit"
            className="w-full"
            disabled={accept.isPending}
          >
            {accept.isPending
              ? t('invitations.accept.submitting')
              : t('invitations.accept.submit')}
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}

function PreviewSummary({
  preview,
}: {
  preview: InvitationPreview
}): React.ReactElement {
  const t = useT()
  const exp = preview.expiresAt
    ? new Date(preview.expiresAt).toLocaleString()
    : '—'
  return (
    <span className="block space-y-1 text-xs">
      <span className="block">
        <span className="text-muted-foreground">
          {t('invitations.accept.preview.email')}:
        </span>{' '}
        <span className="font-mono">{preview.email}</span>
      </span>
      <span className="block">
        <span className="text-muted-foreground">
          {t('invitations.accept.preview.role')}:
        </span>{' '}
        <span className="font-mono uppercase">{preview.roleName}</span>
      </span>
      <span className="block">
        <span className="text-muted-foreground">
          {t('invitations.accept.preview.expires')}:
        </span>{' '}
        {exp}
      </span>
    </span>
  )
}

function ExpiredCard(): React.ReactElement {
  const t = useT()
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('invitations.accept.expired.title')}</CardTitle>
        <CardDescription>
          {t('invitations.accept.expired.description')}
        </CardDescription>
      </CardHeader>
    </Card>
  )
}

function UsedCard(): React.ReactElement {
  const t = useT()
  const navigate = useNavigate()
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('invitations.accept.used.title')}</CardTitle>
        <CardDescription>
          {t('invitations.accept.used.description')}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Button
          type="button"
          variant="outline"
          onClick={() => void navigate({ to: '/login' })}
        >
          {t('invitations.accept.toLogin')}
        </Button>
      </CardContent>
    </Card>
  )
}

function NotFoundCard({ error }: { error: unknown }): React.ReactElement {
  const t = useT()
  const message =
    error instanceof ApiError
      ? error.message
      : t('invitations.accept.error.fallback')
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('invitations.accept.notfound.title')}</CardTitle>
        <CardDescription>
          {t('invitations.accept.notfound.description')}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <p
          className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm text-destructive"
          role="alert"
        >
          {message}
        </p>
      </CardContent>
    </Card>
  )
}

// ────────────────────────────────────────────────────────────────────────
// Helpers (export — 단위 테스트용)
// ────────────────────────────────────────────────────────────────────────

export type InvitationPreviewState = 'active' | 'expired' | 'used'

// invitationPreviewState — preview의 effective 상태 분류.
//   - accepted=true → 'used' (만료 무관, used가 우선).
//   - expiresAt이 과거 → 'expired'.
//   - 그 외 'active'.
export function invitationPreviewState(
  preview: Pick<InvitationPreview, 'accepted' | 'expiresAt'>,
): InvitationPreviewState {
  if (preview.accepted) return 'used'
  if (preview.expiresAt) {
    const exp = new Date(preview.expiresAt).getTime()
    if (Number.isFinite(exp) && exp < Date.now()) {
      return 'expired'
    }
  }
  return 'active'
}

// validateAcceptForm — accept 폼의 클라이언트 측 검증.
//   필드별 에러 키 배열 반환. 빈 배열이면 통과.
//   - email: trim 후 비어있으면 'email'.
//   - displayName: trim 후 비어있으면 'displayName'.
//   - password: 12자 미만이면 'password'.
export function validateAcceptForm(values: {
  email: string
  displayName: string
  password: string
}): string[] {
  const errs: string[] = []
  if (values.email.trim().length === 0) errs.push('email')
  if (values.displayName.trim().length === 0) errs.push('displayName')
  if (values.password.length < 12) errs.push('password')
  return errs
}

function validationErrorKey(field: string): DictKey {
  switch (field) {
    case 'email':
      return 'invitations.accept.validation.email'
    case 'displayName':
      return 'invitations.accept.validation.displayName'
    case 'password':
      return 'invitations.accept.validation.password'
    default:
      return 'invitations.accept.error.fallback'
  }
}

export const Route = createFileRoute('/invitations/accept/$token')({
  component: AcceptInvitationPage,
})
