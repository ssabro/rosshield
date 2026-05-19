// PWA Stage 2/3 — service worker 등록 wrapper + 갱신 알림 hook 결선.
//
// design doc §6.2 + §7 Stage 2/3 — Stage 2에서 SW 등록 + 자리만 마련했고,
// Stage 3에서 onNeedRefresh / onOfflineReady 콜백을 module-level subscribe
// 패턴으로 결선합니다.
//
// 등록 방식:
//   - vite-plugin-pwa의 `virtual:pwa-register` 모듈이 build 시 자동 생성.
//   - `registerSW({...})` 호출 → 브라우저가 SW를 fetch + install + activate.
//   - dev 모드는 `devOptions.enabled=false` (vite.config.ts)라 본 함수가 no-op.
//
// 본 모듈은 SW 등록을 **앱 생애 1회만** 수행하고, 갱신 상태(needRefresh /
// offlineReady)와 reload 함수를 React hook(`usePwaUpdate`)에서 구독할 수 있도록
// module-level subscriber 패턴으로 노출합니다.

interface PwaState {
  needRefresh: boolean
  offlineReady: boolean
  /** updateServiceWorker(true) 호출 → skipWaiting + reload. virtual 모듈 미가용 시 no-op. */
  reload: () => Promise<void>
}

type Listener = (state: PwaState) => void

const NOOP_RELOAD = async (): Promise<void> => {
  // virtual:pwa-register 미주입 환경(test/dev) — no-op.
}

let state: PwaState = {
  needRefresh: false,
  offlineReady: false,
  reload: NOOP_RELOAD,
}
const listeners = new Set<Listener>()
let registered = false

function update(partial: Partial<PwaState>): void {
  state = { ...state, ...partial }
  for (const fn of listeners) {
    fn(state)
  }
}

/**
 * 현재 PWA 상태를 반환합니다 (subscribe 미참여 — snapshot).
 */
export function getPwaState(): PwaState {
  return state
}

/**
 * 상태 변경 알림 구독. 반환값은 unsubscribe 함수.
 */
export function subscribePwa(listener: Listener): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/**
 * Service worker를 등록합니다 (앱 생애 1회).
 *
 * 브라우저가 SW를 지원하지 않거나 production 빌드 외 환경(dev)에서는 no-op.
 * 등록 실패 시 console.warn만 남기고 silent (앱 동작 자체에 영향 0).
 */
export function registerServiceWorker(): void {
  if (registered) {
    return // 중복 등록 방지 (StrictMode 또는 HMR).
  }
  if (typeof window === 'undefined') {
    return // SSR/Node 환경 가드 (Vitest 등).
  }
  if (!('serviceWorker' in navigator)) {
    return // 구형 브라우저 — PWA 미지원.
  }
  registered = true

  // virtual 모듈은 build 시에만 존재 — dev 또는 vitest에선 import 실패 가능.
  // 동적 import로 감싸 silent fallback. import-analysis 우회를 위해 변수에
  // 담아 호출(vitest의 vite-import-analysis가 정적 string을 검사하지 않도록).
  void (async () => {
    try {
      // vite-plugin-pwa가 build 시 transform — @vite-ignore 제거(이 주석이
      // 있으면 vite가 module path 분석을 skip해서 PWA plugin이 transform을
      // 수행하지 못함. production build에서 virtual:pwa-register가 실 module
      // 로 resolve되지 않아 dynamic import가 CORS error로 fail).
      const mod = (await import('virtual:pwa-register')) as {
        registerSW: (options?: {
          immediate?: boolean
          onNeedRefresh?: () => void
          onOfflineReady?: () => void
          onRegisterError?: (error: unknown) => void
        }) => (reloadPage?: boolean) => Promise<void>
      }
      const updateSW = mod.registerSW({
        immediate: true,
        // Stage 3 결선 — 새 SW가 install되어 activate 대기 중.
        onNeedRefresh: () => {
          update({ needRefresh: true })
        },
        // Stage 3 결선 — 첫 설치 완료, 오프라인 사용 가능 (1회 안내).
        onOfflineReady: () => {
          update({ offlineReady: true })
        },
        onRegisterError: (err) => {
          // eslint-disable-next-line no-console
          console.warn('[pwa] service worker register error:', err)
        },
      })
      // reload 함수 결선 — 사용자 클릭 시 needRefresh false + skipWaiting + reload.
      update({
        reload: async () => {
          update({ needRefresh: false })
          await updateSW(true)
        },
      })
    } catch (err) {
      // eslint-disable-next-line no-console
      console.warn('[pwa] virtual:pwa-register unavailable:', err)
    }
  })()
}

/**
 * offlineReady 안내를 닫습니다 (사용자 dismiss).
 *
 * design doc §7 Stage 3 — offline ready 안내는 한 번만 노출. 사용자가 close 버튼
 * 또는 자동 timeout으로 닫을 때 호출.
 */
export function dismissOfflineReady(): void {
  update({ offlineReady: false })
}

/**
 * needRefresh 안내를 닫습니다 (예: 사용자가 "나중에" 선택한 경우).
 */
export function dismissNeedRefresh(): void {
  update({ needRefresh: false })
}

/**
 * 테스트 전용 — module 상태를 초기값으로 되돌립니다.
 *
 * 단위 테스트에서 등록 1회 가드를 회피하고 PWA 상태를 깨끗하게 유지하기 위해
 * 사용. 프로덕션 코드에서는 호출 금지.
 */
export function __resetPwaStateForTests(): void {
  state = {
    needRefresh: false,
    offlineReady: false,
    reload: NOOP_RELOAD,
  }
  listeners.clear()
  registered = false
}

/**
 * 테스트 전용 — needRefresh 상태를 직접 트리거합니다.
 */
export function __triggerNeedRefreshForTests(reload: () => Promise<void>): void {
  update({ needRefresh: true, reload })
}

/**
 * 테스트 전용 — offlineReady 상태를 직접 트리거합니다.
 */
export function __triggerOfflineReadyForTests(): void {
  update({ offlineReady: true })
}
