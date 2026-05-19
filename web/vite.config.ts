import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { TanStackRouterVite } from '@tanstack/router-plugin/vite'
import { VitePWA } from 'vite-plugin-pwa'
import path from 'node:path'

// rosshield Web Console Vite 설정.
// - 빌드 결과는 web/dist/ → Stage D에서 Go embed.FS로 끌어올린다.
// - dev 서버는 5173 고정, /api proxy → localhost:8080(rosshield-server).
// - Phase 5 PWA Stage 2 — vite-plugin-pwa generateSW 모드로 service worker
//   자동 생성 + Workbox precache + manifest 인라인 위임 (design doc §6.2).
export default defineConfig({
  plugins: [
    TanStackRouterVite({
      routesDirectory: './src/routes',
      generatedRouteTree: './src/routeTree.gen.ts',
      autoCodeSplitting: true,
      // *.test.tsx 등 Vitest 파일이 routes/ 안에 있어도 route로 취급하지 않음.
      routeFileIgnorePattern: '\\.test\\.[jt]sx?$',
    }),
    react(),
    tailwindcss(),
    // PWA Stage 2 — Workbox 7 generateSW 모드 (design doc D-PWA-1 권장 default).
    // - registerType=prompt: 사용자 동의로 reload (D-PWA-6, audit 입력 폼 보호).
    // - injectRegister=null: SW 등록을 main.tsx에서 명시적으로 수행
    //   (`virtual:pwa-register`) — Stage 3에서 useRegisterSW hook 결선 예정.
    // - manifest 인라인 위임(D-PWA-2) — 정적 web/public/manifest.webmanifest 제거.
    // - workbox.navigateFallbackDenylist=/api — /api/*는 SW 우회(D-PWA-7,
    //   사용자 토큰 응답 캐시 0 → 멀티테넌시 유출 차단).
    // - runtimeCaching=[]: GET API 캐시는 옵션 C(react-query persist) 별 트랙.
    VitePWA({
      registerType: 'prompt',
      injectRegister: null,
      devOptions: {
        enabled: false,
      },
      manifest: {
        name: 'Lodestar 관리자 콘솔',
        short_name: 'Lodestar',
        description: 'Lodestar — ROS2 robot fleet 보안 감사 플랫폼 (codename rosshield)',
        start_url: '/',
        scope: '/',
        display: 'standalone',
        background_color: '#0a0a0a',
        theme_color: '#0a0a0a',
        icons: [
          { src: '/icon-192.png', sizes: '192x192', type: 'image/png', purpose: 'any' },
          { src: '/icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'any' },
          { src: '/icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
      workbox: {
        // SPA 라우트는 모두 index.html로 fallback (TanStack Router 클라이언트 측 처리).
        navigateFallback: '/index.html',
        // /api/* 는 SW 우회 (응답 캐시 0, 토큰/테넌트 데이터 노출 차단 — D-PWA-7).
        navigateFallbackDenylist: [/^\/api\//],
        // precache 대상 자산 패턴 (Vite hash 자산명은 immutable 안전).
        globPatterns: ['**/*.{js,css,html,svg,png,ico,woff,woff2}'],
        // 갱신된 SW가 활성화되면 구버전 precache 자산 자동 정리.
        cleanupOutdatedCaches: true,
        // 런타임 캐시 정책은 본 stage에서 비움 (옵션 C 진입 시 추가).
        runtimeCaching: [],
      },
    }),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    // E10 Stage D — Go `//go:embed dist/*`가 internal/web/ 안에서 직접 읽도록 outDir을
    // ../internal/web/dist로 두고 emptyOutDir 명시(외부 경로라 Vite가 안전 가드).
    // sourcemap=false — 프로덕션 임베드 부담 회피 (1.5MB 절약). dev 모드는 자동 활성.
    outDir: '../internal/web/dist',
    emptyOutDir: true,
    sourcemap: false,
  },
})
