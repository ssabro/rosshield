// PWA Stage 3 — UpdatePrompt 단위 테스트.
//
// pwa-register module-level state를 test helper로 트리거 → toast 렌더 + reload
// 버튼 클릭 시 reload 함수 호출 검증.

import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  __resetPwaStateForTests,
  __triggerNeedRefreshForTests,
} from '@/lib/pwa-register'

import { UpdatePrompt } from './UpdatePrompt'

describe('UpdatePrompt', () => {
  afterEach(() => {
    __resetPwaStateForTests()
  })

  it('needRefresh=false 일 때는 아무것도 렌더하지 않음', () => {
    __resetPwaStateForTests()
    render(<UpdatePrompt />)
    expect(screen.queryByTestId('update-prompt')).toBeNull()
  })

  it('needRefresh=true 트리거 시 toast + reload 버튼 표시', () => {
    render(<UpdatePrompt />)
    expect(screen.queryByTestId('update-prompt')).toBeNull()

    const reloadFn = vi.fn(async () => {})
    act(() => {
      __triggerNeedRefreshForTests(reloadFn)
    })

    const prompt = screen.getByTestId('update-prompt')
    expect(prompt).toBeInTheDocument()
    // ko/en 어느 locale로 detect되어도 인식되도록 OR 매칭.
    expect(prompt.textContent ?? '').toMatch(/새 버전|new version/i)
    expect(screen.getByTestId('update-prompt-reload')).toBeInTheDocument()
  })

  it('reload 버튼 클릭 시 reload 함수 호출', async () => {
    const reloadFn = vi.fn(async () => {})
    render(<UpdatePrompt />)
    act(() => {
      __triggerNeedRefreshForTests(reloadFn)
    })
    const button = screen.getByTestId('update-prompt-reload')
    await act(async () => {
      fireEvent.click(button)
    })
    expect(reloadFn).toHaveBeenCalledTimes(1)
  })

  it('dismiss 버튼 클릭 시 toast 사라짐', () => {
    render(<UpdatePrompt />)
    act(() => {
      __triggerNeedRefreshForTests(vi.fn(async () => {}))
    })
    expect(screen.getByTestId('update-prompt')).toBeInTheDocument()

    act(() => {
      fireEvent.click(screen.getByTestId('update-prompt-dismiss'))
    })
    expect(screen.queryByTestId('update-prompt')).toBeNull()
  })
})
