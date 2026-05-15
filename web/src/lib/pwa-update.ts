// PWA Stage 3 — service worker 갱신 알림 React hook (design doc §6.4 + §7 Stage 3).
//
// `pwa-register.ts`가 main.tsx에서 SW를 1회 등록한 뒤 module-level subscribe
// 패턴으로 needRefresh / offlineReady / reload 상태를 노출합니다. 본 hook은
// 그 subscribe API를 useSyncExternalStore로 React 트리에 주입합니다.
//
// design doc D-PWA-6 (registerType: 'prompt') 결정에 따라 자동 갱신 0 — 사용자가
// `reload()`를 명시 호출해야만 새 SW가 활성화됩니다(audit 입력 폼 작성 중 데이터
// 손실 방지).

import { useSyncExternalStore } from 'react'

import {
  dismissNeedRefresh,
  dismissOfflineReady,
  getPwaState,
  subscribePwa,
} from './pwa-register'

export interface PwaUpdateState {
  /** 새 SW 발견 + install 완료, activate 대기 중 (사용자 prompt). */
  readonly needRefresh: boolean
  /** 첫 로드에서 SW가 install 완료 → 오프라인 사용 가능 안내용 (1회). */
  readonly offlineReady: boolean
  /** 새로고침 트리거: `updateServiceWorker(true)` 호출 + 페이지 reload. */
  readonly reload: () => Promise<void>
  /** offlineReady 안내를 사용자가 닫을 때 호출. */
  readonly dismissOfflineReady: () => void
  /** needRefresh 안내를 사용자가 닫을 때(나중에 갱신) 호출. */
  readonly dismissNeedRefresh: () => void
}

/**
 * PWA 갱신 + 오프라인 ready 상태 hook.
 *
 * SW 등록은 `registerServiceWorker()` (main.tsx에서 1회) 가 담당하며 본 hook은
 * 결과 상태만 구독합니다. virtual 모듈이 미주입된 환경(dev/vitest)에서는
 * 모든 상태가 false, reload가 no-op으로 안전 동작합니다.
 */
export function usePwaUpdate(): PwaUpdateState {
  const state = useSyncExternalStore(subscribePwa, getPwaState, getPwaState)

  return {
    needRefresh: state.needRefresh,
    offlineReady: state.offlineReady,
    reload: state.reload,
    dismissOfflineReady,
    dismissNeedRefresh,
  }
}
