// PWA Stage 3 — useIsOffline hook 단위 테스트.
//
// navigator.onLine 초기값 + online/offline 이벤트 발화 시 상태 갱신을 검증합니다.

import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { mutationGuardTitle, useIsOffline } from './use-is-offline'

describe('useIsOffline', () => {
  // navigator.onLine은 read-only이지만 jsdom에서 Object.defineProperty로 mock 가능.
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

  it('navigator.onLine === true 일 때 false (=온라인)', () => {
    setOnLine(true)
    const { result } = renderHook(() => useIsOffline())
    expect(result.current).toBe(false)
  })

  it('navigator.onLine === false 일 때 true (=오프라인)', () => {
    setOnLine(false)
    const { result } = renderHook(() => useIsOffline())
    expect(result.current).toBe(true)
  })

  it('offline 이벤트 발화 시 true로 전환', () => {
    setOnLine(true)
    const { result } = renderHook(() => useIsOffline())
    expect(result.current).toBe(false)

    act(() => {
      setOnLine(false)
      window.dispatchEvent(new Event('offline'))
    })
    expect(result.current).toBe(true)
  })

  it('online 이벤트 발화 시 false로 전환', () => {
    setOnLine(false)
    const { result } = renderHook(() => useIsOffline())
    expect(result.current).toBe(true)

    act(() => {
      setOnLine(true)
      window.dispatchEvent(new Event('online'))
    })
    expect(result.current).toBe(false)
  })

  it('unmount 시 listener 누수 0 (반복 mount/unmount 후 dispatch 무영향)', () => {
    setOnLine(true)
    const { result, unmount } = renderHook(() => useIsOffline())
    unmount()
    // unmount 후 dispatch는 hook state를 더 이상 갱신하지 않아야 함.
    act(() => {
      setOnLine(false)
      window.dispatchEvent(new Event('offline'))
    })
    // result.current는 unmount 시점 값(false)을 유지.
    expect(result.current).toBe(false)
  })
})

describe('mutationGuardTitle (PWA Stage 4 — D-PWA-4 우선순위)', () => {
  it('isOffline=true이면 offlineLabel 반환 (fallback 무시)', () => {
    expect(
      mutationGuardTitle({
        isOffline: true,
        offlineLabel: '오프라인 — 변경 불가',
        fallback: 'admin 권한 필요',
      }),
    ).toBe('오프라인 — 변경 불가')
  })

  it('isOffline=false + fallback 정의되면 fallback 반환', () => {
    expect(
      mutationGuardTitle({
        isOffline: false,
        offlineLabel: '오프라인 — 변경 불가',
        fallback: 'admin 권한 필요',
      }),
    ).toBe('admin 권한 필요')
  })

  it('isOffline=false + fallback=undefined이면 undefined (tooltip 비표시)', () => {
    expect(
      mutationGuardTitle({
        isOffline: false,
        offlineLabel: '오프라인 — 변경 불가',
        fallback: undefined,
      }),
    ).toBeUndefined()
  })

  it('isOffline=true + fallback=undefined이어도 offlineLabel 반환 (offline 우선)', () => {
    expect(
      mutationGuardTitle({
        isOffline: true,
        offlineLabel: '오프라인 — 변경 불가',
      }),
    ).toBe('오프라인 — 변경 불가')
  })
})
