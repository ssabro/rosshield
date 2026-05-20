import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { ShieldCheck } from 'lucide-react'
import { useState } from 'react'

import { ApiError } from '@/api/errors'
import { useLogin } from '@/api/hooks'
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

import type { FormEvent } from 'react'

// `/login` — Web Console의 인증 진입점.
// - 자체 풀스크린 레이아웃(Sidebar/Header 없음).
// - 성공 시 `/robots`로 navigate. 실패 시 ApiError 내용을 한국어 메시지로 매핑.
// - 인증 가드 적용 X — `_authenticated`만 token 체크한다.
//
// D-UI-1 Stage 5b additional — axe scan 테스트 mount용 named export.
//   route component는 그대로 (createFileRoute가 내부 참조). 외부 호출은
//   `__tests__/a11y-additional.test.tsx` 만.
export function LoginPage(): React.ReactElement {
  const navigate = useNavigate()
  const login = useLogin()
  const t = useT()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')

  const handleSubmit = (e: FormEvent<HTMLFormElement>): void => {
    e.preventDefault()
    setError('')
    login.mutate(
      { email, password },
      {
        onSuccess: () => {
          void navigate({ to: '/robots' })
        },
        onError: (err) => {
          if (err instanceof ApiError && err.isUnauthorized()) {
            setError(t('login.error.invalid'))
          } else {
            setError(err instanceof Error ? err.message : t('login.error.invalid'))
          }
        },
      },
    )
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-gradient-to-br from-muted via-background to-muted px-4">
      <div className="w-full max-w-md space-y-6">
        <div className="flex flex-col items-center gap-3 text-center">
          <div className="rounded-xl bg-primary/10 p-3">
            <ShieldCheck className="h-8 w-8 text-primary" aria-hidden />
          </div>
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">{t('app.brand')}</h1>
            <p className="text-sm text-muted-foreground">
              ROS2 Fleet Security Console
            </p>
          </div>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>{t('login.title')}</CardTitle>
            <CardDescription>{t('login.description')}</CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit} className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="email">{t('login.email')}</Label>
                <Input
                  id="email"
                  type="email"
                  required
                  autoComplete="email"
                  autoFocus
                  placeholder="admin@example.com"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password">{t('login.password')}</Label>
                <Input
                  id="password"
                  type="password"
                  required
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>
              {error && (
                <p
                  className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm text-destructive"
                  role="alert"
                >
                  {error}
                </p>
              )}
              <Button
                type="submit"
                className="w-full"
                disabled={login.isPending}
              >
                {login.isPending ? t('login.submitting') : t('login.submit')}
              </Button>
            </form>
          </CardContent>
        </Card>

        <p className="text-center text-xs text-muted-foreground">
          {t('login.footer')}
        </p>
      </div>
    </div>
  )
}

export const Route = createFileRoute('/login')({
  component: LoginPage,
})
