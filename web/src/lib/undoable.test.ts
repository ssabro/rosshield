// undoableAction 단위 테스트.
//
// vi.useFakeTimers + sonner mock으로 setTimeout 흐름과 toast.action.onClick 분기를
// 결정론적으로 검증한다. toast 호출은 호출 횟수·인자만 검증하고 실제 DOM 렌더는
// 본 단위 테스트의 책임 밖(통합/E2E).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// toast wrapper를 통째로 mock — sonner DOM 미마운트.
vi.mock('@/lib/toast', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
    warning: vi.fn(),
    message: vi.fn(),
    promise: vi.fn(),
    dismiss: vi.fn(),
  },
}))

import { toast } from '@/lib/toast'
import { undoableAction } from './undoable'

describe('undoableAction', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('delayMs 경과 시 action 실행', () => {
    const action = vi.fn()
    undoableAction({ message: '삭제 진행', action, delayMs: 5000 })
    expect(action).not.toHaveBeenCalled()
    vi.advanceTimersByTime(4999)
    expect(action).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(action).toHaveBeenCalledTimes(1)
  })

  it('undo click 시 action 미실행 + onUndo 호출', () => {
    const action = vi.fn()
    const onUndo = vi.fn()
    undoableAction({ message: '삭제 진행', action, onUndo, delayMs: 5000 })

    // toast.success로 넘어간 action.onClick을 직접 트리거.
    expect(toast.success).toHaveBeenCalledTimes(1)
    const successCall = (toast.success as ReturnType<typeof vi.fn>).mock.calls[0]
    const opts = successCall[1] as { action: { onClick: () => void } }
    opts.action.onClick()

    expect(onUndo).toHaveBeenCalledTimes(1)
    expect(toast.info).toHaveBeenCalledWith('취소되었습니다')

    // delay 경과 후에도 action은 실행되지 않아야 한다.
    vi.advanceTimersByTime(10_000)
    expect(action).not.toHaveBeenCalled()
  })

  it('undo 두 번 click 해도 onUndo는 1회만', () => {
    const action = vi.fn()
    const onUndo = vi.fn()
    undoableAction({ message: 'X', action, onUndo, delayMs: 5000 })
    const successCall = (toast.success as ReturnType<typeof vi.fn>).mock.calls[0]
    const opts = successCall[1] as { action: { onClick: () => void } }
    opts.action.onClick()
    opts.action.onClick()
    expect(onUndo).toHaveBeenCalledTimes(1)
    expect(toast.info).toHaveBeenCalledTimes(1)
  })

  it('action이 Promise를 reject하면 toast.error 호출', async () => {
    const action = vi.fn().mockRejectedValue(new Error('boom'))
    undoableAction({ message: 'X', action, delayMs: 1000 })
    vi.advanceTimersByTime(1000)
    expect(action).toHaveBeenCalledTimes(1)
    // microtask flush — Promise rejection 처리.
    await vi.runAllTimersAsync()
    expect(toast.error).toHaveBeenCalledWith('실패: boom')
  })

  it('action이 동기 throw하면 toast.error 호출', () => {
    const action = vi.fn(() => {
      throw new Error('sync-boom')
    })
    undoableAction({ message: 'X', action, delayMs: 1000 })
    vi.advanceTimersByTime(1000)
    expect(toast.error).toHaveBeenCalledWith('실패: sync-boom')
  })

  it('default delayMs=5000 사용 시 description에 5초 표기', () => {
    undoableAction({ message: 'X', action: vi.fn() })
    const successCall = (toast.success as ReturnType<typeof vi.fn>).mock.calls[0]
    const opts = successCall[1] as { description: string }
    expect(opts.description).toBe('5초 후 적용됩니다')
  })

  it('description 명시 시 default 미사용', () => {
    undoableAction({
      message: 'X',
      description: 'custom desc',
      action: vi.fn(),
    })
    const successCall = (toast.success as ReturnType<typeof vi.fn>).mock.calls[0]
    const opts = successCall[1] as { description: string }
    expect(opts.description).toBe('custom desc')
  })

  it('undoLabel 명시 시 toast.action.label에 반영', () => {
    undoableAction({
      message: 'X',
      action: vi.fn(),
      undoLabel: 'Undo',
    })
    const successCall = (toast.success as ReturnType<typeof vi.fn>).mock.calls[0]
    const opts = successCall[1] as { action: { label: string } }
    expect(opts.action.label).toBe('Undo')
  })

  it('다중 호출 시 각 instance가 독립적으로 cancel/실행', () => {
    const actionA = vi.fn()
    const actionB = vi.fn()
    const onUndoA = vi.fn()

    undoableAction({ message: 'A', action: actionA, onUndo: onUndoA, delayMs: 5000 })
    undoableAction({ message: 'B', action: actionB, delayMs: 5000 })

    // A만 undo.
    const calls = (toast.success as ReturnType<typeof vi.fn>).mock.calls
    expect(calls).toHaveLength(2)
    const optsA = calls[0][1] as { action: { onClick: () => void } }
    optsA.action.onClick()

    vi.advanceTimersByTime(5000)
    expect(actionA).not.toHaveBeenCalled()
    expect(actionB).toHaveBeenCalledTimes(1)
    expect(onUndoA).toHaveBeenCalledTimes(1)
  })

  it('errorLabel 명시 시 toast.error prefix에 사용', async () => {
    const action = vi.fn().mockRejectedValue(new Error('x'))
    undoableAction({
      message: 'X',
      action,
      errorLabel: '삭제 실패',
      delayMs: 1000,
    })
    vi.advanceTimersByTime(1000)
    await vi.runAllTimersAsync()
    expect(toast.error).toHaveBeenCalledWith('삭제 실패: x')
  })
})
