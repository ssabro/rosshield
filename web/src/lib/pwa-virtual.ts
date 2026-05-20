// vite-plugin-pwa의 `virtual:pwa-register` 모듈 로더를 별 파일로 격리합니다.
// - production build: 정적 `import` 문장을 PWA plugin이 transform → dist의
//   실 SW 등록 helper로 resolve.
// - vitest: 본 모듈 전체를 vi.mock으로 가로채면(`web/src/test/setup.ts`),
//   pwa-register.ts의 import-analysis가 'virtual:pwa-register'에 직접 닿지 않습니다.

export interface RegisterSWOptions {
  immediate?: boolean
  onNeedRefresh?: () => void
  onOfflineReady?: () => void
  onRegisterError?: (error: unknown) => void
}

export type RegisterSW = (options?: RegisterSWOptions) => (reloadPage?: boolean) => Promise<void>

export async function loadRegisterSW(): Promise<RegisterSW> {
  const mod = (await import('virtual:pwa-register')) as { registerSW: RegisterSW }
  return mod.registerSW
}
