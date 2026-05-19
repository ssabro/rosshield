import { describe, expect, it } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

import { ConfirmDialogHost, confirm } from './ConfirmDialog'

// ConfirmDialog imperative `confirm()` API + Host 결선 + typing confirmation.

describe('confirm() imperative API', () => {
  it('resolves false when host is not mounted', async () => {
    const result = await confirm({ title: '테스트' })
    expect(result).toBe(false)
  })

  it('resolves true on action click', async () => {
    const user = userEvent.setup()
    render(<ConfirmDialogHost />)
    let resolved: boolean | null = null
    act(() => {
      void confirm({
        title: '로봇 삭제',
        description: '되돌릴 수 없습니다',
        confirmLabel: '삭제',
      }).then((r) => {
        resolved = r
      })
    })
    expect(await screen.findByText('로봇 삭제')).toBeInTheDocument()
    expect(screen.getByText('되돌릴 수 없습니다')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '삭제' }))
    expect(resolved).toBe(true)
  })

  it('resolves false on cancel click', async () => {
    const user = userEvent.setup()
    render(<ConfirmDialogHost />)
    let resolved: boolean | null = null
    act(() => {
      void confirm({ title: '확인이 필요한 작업' }).then((r) => {
        resolved = r
      })
    })
    await screen.findByText('확인이 필요한 작업')
    await user.click(screen.getByRole('button', { name: '취소' }))
    expect(resolved).toBe(false)
  })

  it('disables action until typing confirmation matches', async () => {
    const user = userEvent.setup()
    render(<ConfirmDialogHost />)
    act(() => {
      void confirm({
        title: '치명적 삭제',
        confirmText: 'DELETE',
        destructive: true,
        confirmLabel: '확정',
      })
    })
    await screen.findByText('치명적 삭제')
    const action = screen.getByRole('button', { name: '확정' })
    expect(action).toBeDisabled()
    await user.type(screen.getByRole('textbox'), 'DELETE')
    expect(action).not.toBeDisabled()
  })
})
