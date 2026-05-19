import { useEffect, useState } from 'react'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { buttonVariants } from '@/components/ui/button'
import { cn } from '@/lib/utils'

// D-UI-1 Stage 2 — ConfirmDialog + imperative `confirm()` Promise API.
//
// 사용처: window.confirm 대체 (a11y/security review P0 — 7곳).
// destructive 작업은 명시적 typing confirmation (예: "DELETE" 타이핑) 옵션으로
// 실수 차단 강도를 높인다. shadcn AlertDialog 기반 (Radix UI, focus trap + ESC).
//
// 사용법:
//   const ok = await confirm({
//     title: '로봇 삭제',
//     description: '이 작업은 되돌릴 수 없습니다.',
//     destructive: true,
//     confirmText: 'DELETE',  // typing confirmation
//   })
//   if (!ok) return
//
// Provider: `<ConfirmDialogHost />`를 `App.tsx` 1회 마운트.

export interface ConfirmOptions {
  title: string
  description?: string
  // 확정 버튼 라벨 (typing confirmation 비활성 시) 또는 타이핑해야 할 문자열.
  // 본 wrapper는 단순화를 위해 `confirmText` 자체를 사용자가 input에 입력해야
  // 확정 가능한 토큰으로 사용한다. 일반 확인은 confirmText 생략.
  confirmText?: string
  // 일반 확인 버튼에 보일 라벨 (기본 '확인').
  confirmLabel?: string
  // 취소 버튼 라벨 (기본 '취소').
  cancelLabel?: string
  // destructive면 확정 버튼이 destructive 스타일.
  destructive?: boolean
}

interface InternalRequest extends ConfirmOptions {
  resolve: (value: boolean) => void
}

let dispatcher: ((req: InternalRequest) => void) | null = null

// Imperative API — Promise 기반.
// `ConfirmDialogHost`가 마운트되지 않은 환경(예: SSR, 일부 test)에서는
// 즉시 false를 반환하여 안전하게 동작.
export function confirm(opts: ConfirmOptions): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    if (!dispatcher) {
      resolve(false)
      return
    }
    dispatcher({ ...opts, resolve })
  })
}

export function ConfirmDialogHost(): React.ReactElement {
  const [req, setReq] = useState<InternalRequest | null>(null)
  const [typed, setTyped] = useState('')

  useEffect(() => {
    dispatcher = (next): void => {
      setTyped('')
      setReq(next)
    }
    return () => {
      dispatcher = null
    }
  }, [])

  const close = (result: boolean): void => {
    if (req) req.resolve(result)
    setReq(null)
    setTyped('')
  }

  const needsTyping = !!req?.confirmText && req.confirmText.length > 0
  const typingOk = needsTyping ? typed === req?.confirmText : true
  const confirmLabel = req?.confirmLabel ?? '확인'
  const cancelLabel = req?.cancelLabel ?? '취소'

  return (
    <AlertDialog
      open={req !== null}
      onOpenChange={(open) => {
        if (!open) close(false)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{req?.title ?? ''}</AlertDialogTitle>
          {req?.description && (
            <AlertDialogDescription>{req.description}</AlertDialogDescription>
          )}
        </AlertDialogHeader>
        {needsTyping && req && (
          <div className="space-y-2">
            <Label htmlFor="confirm-typing-input">
              계속하려면{' '}
              <code className="rounded bg-muted px-1 py-0.5 text-xs font-semibold">
                {req.confirmText}
              </code>
              를 입력하세요.
            </Label>
            <Input
              id="confirm-typing-input"
              autoFocus
              autoComplete="off"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              placeholder={req.confirmText}
            />
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => close(false)}>
            {cancelLabel}
          </AlertDialogCancel>
          <AlertDialogAction
            onClick={() => close(true)}
            disabled={!typingOk}
            className={cn(
              req?.destructive &&
                buttonVariants({ variant: 'destructive' }),
            )}
          >
            {confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
