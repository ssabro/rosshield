// PWA Stage 3 — 오프라인 상태 React hook (design doc §6.5 + §7 Stage 3).
//
// `navigator.onLine` 초기값을 읽고 `online`/`offline` 이벤트 리스너로 상태를
// 동기화합니다. SSR/Vitest jsdom 환경에서 navigator가 없거나 onLine 정의가
// 부재한 경우는 false(=온라인)로 간주해 SSR-safe 합니다.
//
// 주의: navigator.onLine은 OS/브라우저별로 신뢰도 차이가 있어(랜선만 빠진 경우
// 일부 OS는 true 유지) "강한 보장"이 아닌 "사용자 안내 신호"로만 사용합니다.
// mutation 차단 등 강한 가드는 Stage 4에서 fetch 실패 fallback과 함께 결정.

import { useEffect, useState } from 'react'

/**
 * 현재 브라우저가 오프라인인지 여부를 반환합니다.
 *
 * - 초기값: `navigator.onLine === false` (Node/SSR/jsdom 환경에선 false=온라인 가정).
 * - `online`/`offline` 이벤트 리스너로 자동 갱신, unmount 시 제거.
 */
export function useIsOffline(): boolean {
  const [offline, setOffline] = useState<boolean>(() => readInitialOffline())

  useEffect(() => {
    if (typeof window === 'undefined') {
      return
    }
    const handleOnline = (): void => setOffline(false)
    const handleOffline = (): void => setOffline(true)
    window.addEventListener('online', handleOnline)
    window.addEventListener('offline', handleOffline)
    // mount 시점에서 한 번 sync — 초기값 읽은 이후 navigator 상태가 바뀌었을 수 있음.
    setOffline(readInitialOffline())
    return () => {
      window.removeEventListener('online', handleOnline)
      window.removeEventListener('offline', handleOffline)
    }
  }, [])

  return offline
}

function readInitialOffline(): boolean {
  if (typeof navigator === 'undefined') {
    return false
  }
  // navigator.onLine이 정의되지 않은 환경에선 온라인 가정.
  if (typeof navigator.onLine !== 'boolean') {
    return false
  }
  return !navigator.onLine
}

// PWA Stage 4 — mutation 버튼 가드 tooltip 우선순위 헬퍼 (design doc §7 Stage 4 + D-PWA-4).
//
// 같은 mutation 버튼이 다중 사유로 비활성될 수 있어(role 미보유 + 오프라인 등),
// 사용자에게 보여줄 단일 tooltip 문자열을 우선순위 고정으로 결정합니다.
// 우선순위: offline > 그 외 사유. 차단력이 가장 강하고 즉시 회복 가능한 사유를 우선.
//
// 사용 예:
//   <Button
//     disabled={create.isPending || !isAdmin || isOffline}
//     title={mutationGuardTitle({
//       isOffline,
//       offlineLabel: t('pwa.offline.mutationBlocked'),
//       fallback: !isAdmin ? t('common.role.required.admin') : undefined,
//     })}
//   />
//
// fallback가 undefined이고 isOffline=false면 undefined 반환(=tooltip 비표시).
export interface MutationGuardTitleOpts {
  isOffline: boolean
  offlineLabel: string
  /** offline이 아닐 때 사용할 보조 사유 tooltip (예: role 미보유). */
  fallback?: string | undefined
}

export function mutationGuardTitle(
  opts: MutationGuardTitleOpts,
): string | undefined {
  if (opts.isOffline) {
    return opts.offlineLabel
  }
  return opts.fallback
}
