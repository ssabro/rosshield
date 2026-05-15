// PWA Stage 2 — service worker 등록 wrapper.
//
// design doc §6.2 + §7 Stage 2 — 본 stage는 SW 등록만 담당합니다.
// 갱신 알림 UX(`useRegisterSW` hook + UpdatePrompt + offlineReady toast)는
// Stage 3에서 추가됩니다.
//
// 등록 방식:
//   - vite-plugin-pwa의 `virtual:pwa-register` 모듈이 build 시 자동 생성.
//   - `registerSW({...})` 호출 → 브라우저가 SW를 fetch + install + activate.
//   - dev 모드는 `devOptions.enabled=false` (vite.config.ts)라 본 함수가 no-op.
//
// 본 모듈은 다음을 의도적으로 노출하지 않습니다 (Stage 3 영역):
//   - needRefresh / offlineReady 상태
//   - updateSW(true) 호출 (사용자 prompt 동의 후 reload)
//   - React hook 어댑터 (`useRegisterSW`)

/**
 * Service worker를 등록합니다.
 *
 * 브라우저가 SW를 지원하지 않거나 production 빌드 외 환경(dev)에서는 no-op.
 * 등록 실패 시 console.warn만 남기고 silent (앱 동작 자체에 영향 0).
 */
export function registerServiceWorker(): void {
  if (typeof window === 'undefined') {
    return // SSR/Node 환경 가드 (Vitest 등).
  }
  if (!('serviceWorker' in navigator)) {
    return // 구형 브라우저 — PWA 미지원.
  }

  // virtual 모듈은 build 시에만 존재 — dev 또는 vitest에선 import 실패 가능.
  // 동적 import로 감싸 silent fallback.
  void (async () => {
    try {
      const mod = (await import('virtual:pwa-register')) as {
        registerSW: (options?: {
          immediate?: boolean
          onNeedRefresh?: () => void
          onOfflineReady?: () => void
          onRegisterError?: (error: unknown) => void
        }) => (reloadPage?: boolean) => Promise<void>
      }
      mod.registerSW({
        immediate: true,
        onRegisterError: (err) => {
          // eslint-disable-next-line no-console
          console.warn('[pwa] service worker register error:', err)
        },
        // onNeedRefresh / onOfflineReady 결선은 Stage 3.
      })
    } catch (err) {
      // eslint-disable-next-line no-console
      console.warn('[pwa] virtual:pwa-register unavailable:', err)
    }
  })()
}
