/// <reference types="vitest" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { TanStackRouterVite } from '@tanstack/router-plugin/vite'
import { VitePWA } from 'vite-plugin-pwa'
import path from 'node:path'

// vitest는 vite dev server mode로 모듈을 로드하지만 PWA plugin의 devOptions.enabled=false
// 라 `virtual:pwa-register`가 등록되지 않아 import-analysis 단계에서 fail합니다.
// 해결: vitest 환경에서만 devOptions.enabled=true로 virtual module을 노출시킵니다.
// (vitest는 SW 등록을 실제 수행하지 않으므로 부수효과 0 — pwa-register.ts의 typeof
// window·serviceWorker 가드가 처리.)
const isVitest = process.env.VITEST === 'true' || process.env.NODE_ENV === 'test'

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
        enabled: isVitest,
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
    rollupOptions: {
      output: {
        // v0.6.8 한계 해소 — 단일 main chunk(835KB / gzip 245KB) 분할.
        // 첫 페이지 cold start latency 단축 + browser parallel fetch 활용 +
        // 차후 vendor 업데이트 시 cache invalidation 범위 최소화.
        //
        // 분할 정책:
        //   1) node_modules 만 vendor chunk로 보냄 — 도메인 코드는 main + lazy
        //      route chunk(TanStack autoCodeSplitting)로 자연 분할되도록 유지.
        //   2) 'virtual:pwa-register' 등 가상 모듈은 id 매칭에서 제외 — vitest
        //      mock과 SW 등록 경로를 깨뜨리지 않게 node_modules path filter.
        //   3) 각 그룹은 200~300KB 미만 목표. 초과 시 추가 세분화.
        manualChunks(id) {
          if (!id.includes('node_modules')) {
            return undefined
          }
          // React 코어 — 가장 안정적이고 모든 page에서 참조되므로 별도 chunk로
          // 장기 캐시 효과 극대화.
          if (
            id.includes('node_modules/react/') ||
            id.includes('node_modules/react-dom/') ||
            id.includes('node_modules/scheduler/')
          ) {
            return 'react-vendor'
          }
          // TanStack Router 계열 — autoCodeSplitting과 별도로 router runtime은
          // 모든 라우트 진입에 필요.
          if (
            id.includes('node_modules/@tanstack/react-router') ||
            id.includes('node_modules/@tanstack/router-')
          ) {
            return 'router-vendor'
          }
          // TanStack Query + persist client — 데이터 fetch 레이어. persist는 옵션
          // C 트랙이지만 devDeps로 이미 로드돼 main에 포함되므로 함께 묶음.
          if (id.includes('node_modules/@tanstack/')) {
            return 'query-vendor'
          }
          // Radix UI primitive 17종 — 합계 가장 큰 그룹이지만 모두 헤드리스
          // primitive라 함께 묶어도 트리쉐이킹 영향이 적음.
          if (id.includes('node_modules/@radix-ui/')) {
            return 'radix-vendor'
          }
          // 폼 스택 — react-hook-form + zod + resolvers. 폼 페이지에서만 무거움.
          if (
            id.includes('node_modules/react-hook-form') ||
            id.includes('node_modules/@hookform/') ||
            id.includes('node_modules/zod')
          ) {
            return 'form-vendor'
          }
          // UI 보조 라이브러리 — 토스트·아이콘·variant 유틸. lucide-react가 크기
          // 큰 편이라 별도 분리.
          if (
            id.includes('node_modules/cmdk') ||
            id.includes('node_modules/sonner') ||
            id.includes('node_modules/lucide-react') ||
            id.includes('node_modules/class-variance-authority') ||
            id.includes('node_modules/clsx') ||
            id.includes('node_modules/tailwind-merge')
          ) {
            return 'ui-vendor'
          }
          // 전역 상태 — zustand는 작지만 의미 단위 분리.
          if (id.includes('node_modules/zustand')) {
            return 'state-vendor'
          }
          // 나머지 node_modules (openapi-fetch, workbox-window, pretendard 등)는
          // 기본 vendor chunk로 묶어 main 오염 방지.
          return 'vendor'
        },
      },
    },
  },
})
