// PWA Stage 3 — OfflineIndicator 단위 테스트.
//
// navigator.onLine mock + render 결과 (banner 표시/숨김) 검증.

import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { OfflineIndicator } from './OfflineIndicator'

describe('OfflineIndicator', () => {
  let originalDescriptor: PropertyDescriptor | undefined

  beforeEach(() => {
    originalDescriptor = Object.getOwnPropertyDescriptor(
      window.navigator,
      'onLine',
    )
  })

  afterEach(() => {
    if (originalDescriptor) {
      Object.defineProperty(window.navigator, 'onLine', originalDescriptor)
    }
  })

  function setOnLine(value: boolean): void {
    Object.defineProperty(window.navigator, 'onLine', {
      configurable: true,
      get: () => value,
    })
  }

  it('온라인 상태에서는 아무것도 렌더하지 않음', () => {
    setOnLine(true)
    render(<OfflineIndicator />)
    expect(screen.queryByTestId('offline-indicator')).toBeNull()
  })

  it('오프라인 상태에서는 banner 표시 + 메시지 노출 (locale 무관)', () => {
    setOnLine(false)
    render(<OfflineIndicator />)
    const banner = screen.getByTestId('offline-indicator')
    expect(banner).toBeInTheDocument()
    // jsdom navigator.language는 'en' fallback이므로 ko/en 메시지 모두 허용.
    expect(banner.textContent ?? '').toMatch(/오프라인|Offline/)
  })

  it('online 이벤트 발화 시 banner 숨김', () => {
    setOnLine(false)
    render(<OfflineIndicator />)
    expect(screen.getByTestId('offline-indicator')).toBeInTheDocument()

    act(() => {
      setOnLine(true)
      window.dispatchEvent(new Event('online'))
    })
    expect(screen.queryByTestId('offline-indicator')).toBeNull()
  })
})
