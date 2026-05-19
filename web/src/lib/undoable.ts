// D-UI-1 P0 — destructive action용 Undo window helper.
//
// 의도:
//   ConfirmDialog로 사용자 의도를 한 번 확인한 직후, 추가로 5초 짜리 sonner
//   toast(Undo button)를 띄워 즉시 실제 실행을 보류한다. 5초 안에 "되돌리기"
//   버튼을 누르면 action은 **호출조차 되지 않는다**(서버 round-trip 불필요).
//   5초가 지나면 action()을 실행하고, 실패 시 toast.error로 보고.
//
// 디자인 메모:
//   - destructive UX 가이드(Material/Nielsen)와 Gmail Undo Send 모델 차용.
//   - Optimistic update와 결합 시 ConfirmDialog → 즉시 optimistic disappear
//     → toast Undo가 있으나 본 시점엔 이미 setQueriesData가 적용된 상태이므로
//     onUndo callback에서 rollback을 명시적으로 호출해야 한다. 단순 delete
//     hook은 onUndo 없이 사용 가능(action() 자체를 보류).
//   - setTimeout id를 closure로 캡처하면 Undo 시 clearTimeout으로 깔끔히 정리
//     가능하지만, 본 구현은 cancelled flag만으로 충분 (5초 후 no-op).
//   - 호출 측은 fire-and-forget — Promise 반환하지 않는다 (UX는 toast가 책임).

import { toast } from '@/lib/toast'

export interface UndoableOptions {
  /** toast 본문(성공형). */
  message: string
  /** toast 보조 설명. 미지정 시 "{seconds}초 후 적용됩니다" 형식의 default 표시. */
  description?: string
  /** 실제 실행 함수. delayMs 내 undo가 발생하지 않으면 호출된다. */
  action: () => Promise<unknown> | unknown
  /** undo 가능 시간 (ms). default 5000. */
  delayMs?: number
  /** undo button label. default '되돌리기'. */
  undoLabel?: string
  /** undo click 시 추가로 실행할 callback (optimistic rollback 등). */
  onUndo?: () => void
  /** action() 실패 시 toast.error 본문 prefix. default '실패'. */
  errorLabel?: string
}

/**
 * 5초(=delayMs) 지연 후 action을 실행하고, 그 사이 사용자가 toast의
 * 'Undo' 버튼을 누르면 실행을 취소한다.
 *
 * - delayMs 안에 click → cancelled=true → action 미실행 + onUndo() 호출
 * - delayMs 후 → action() 실행, 실패 시 toast.error
 */
export function undoableAction(opts: UndoableOptions): void {
  const delay = opts.delayMs ?? 5000
  const undoLabel = opts.undoLabel ?? '되돌리기'
  const errorLabel = opts.errorLabel ?? '실패'
  const description =
    opts.description ?? `${Math.round(delay / 1000)}초 후 적용됩니다`

  let cancelled = false

  toast.success(opts.message, {
    description,
    duration: delay,
    action: {
      label: undoLabel,
      onClick: () => {
        if (cancelled) return
        cancelled = true
        opts.onUndo?.()
        toast.info('취소되었습니다')
      },
    },
  })

  setTimeout(() => {
    if (cancelled) return
    try {
      const result = opts.action()
      if (result instanceof Promise) {
        result.catch((err) => {
          const msg = err instanceof Error ? err.message : '알 수 없는 오류'
          toast.error(`${errorLabel}: ${msg}`)
        })
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : '알 수 없는 오류'
      toast.error(`${errorLabel}: ${msg}`)
    }
  }, delay)
}
