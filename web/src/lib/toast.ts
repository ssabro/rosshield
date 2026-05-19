// D-UI-1 Stage 2 — Toast wrapper (sonner 통합).
//
// 사용처: mutation 성공/실패, 백그라운드 작업 완료, 비차단 알림 등.
// 모든 toast 호출은 본 wrapper를 경유하여 sonner API 표면을 1곳에 격리한다 —
// 향후 다른 toast 라이브러리로 교체할 때 호출지를 손대지 않도록.
//
// 디자인 원칙:
//   - 변경 작업(mutation) 결과는 toast로 비차단 통지.
//   - 차단형 확인(destructive)은 `lib/confirm`의 AlertDialog 사용.
//   - sonner의 promise API로 pending → success/error 자동 전환 가능.
//
// Provider: `App.tsx`에서 `<Toaster richColors closeButton />` 1회 마운트.

import { toast as sonnerToast, type ExternalToast } from 'sonner'

export type ToastOptions = ExternalToast

type ToastFn = (msg: string, opts?: ToastOptions) => string | number

export interface PromiseToastMessages<T> {
  loading: string
  success: string | ((data: T) => string)
  error: string | ((err: unknown) => string)
}

export const toast = {
  success: ((msg, opts) => sonnerToast.success(msg, opts)) as ToastFn,
  error: ((msg, opts) => sonnerToast.error(msg, opts)) as ToastFn,
  warning: ((msg, opts) => sonnerToast.warning(msg, opts)) as ToastFn,
  info: ((msg, opts) => sonnerToast.info(msg, opts)) as ToastFn,
  message: ((msg, opts) => sonnerToast(msg, opts)) as ToastFn,
  promise: <T>(promise: Promise<T>, messages: PromiseToastMessages<T>): void => {
    sonnerToast.promise(promise, messages)
  },
  dismiss: (id?: string | number): void => {
    sonnerToast.dismiss(id)
  },
}
