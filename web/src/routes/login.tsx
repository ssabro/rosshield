import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'

import { ApiError } from '@/api/errors'
import { useLogin } from '@/api/hooks'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
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
function LoginPage(): React.ReactElement {
  const navigate = useNavigate()
  const login = useLogin()
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
            setError('이메일 또는 패스워드가 올바르지 않습니다')
          } else {
            setError(err instanceof Error ? err.message : '로그인 실패')
          }
        },
      },
    )
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted px-4">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>rosshield 로그인</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="email">이메일</Label>
              <Input
                id="email"
                type="email"
                required
                autoComplete="email"
                autoFocus
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">패스워드</Label>
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
              <p className="text-sm text-destructive" role="alert">
                {error}
              </p>
            )}
            <Button
              type="submit"
              className="w-full"
              disabled={login.isPending}
            >
              {login.isPending ? '로그인 중…' : '로그인'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}

export const Route = createFileRoute('/login')({
  component: LoginPage,
})
