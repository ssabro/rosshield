import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { TanStackRouterVite } from '@tanstack/router-plugin/vite'
import path from 'node:path'

// rosshield Web Console Vite 설정.
// - 빌드 결과는 web/dist/ → Stage D에서 Go embed.FS로 끌어올린다.
// - dev 서버는 5173 고정, /api proxy → localhost:8080(rosshield-server).
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
