import { defineConfig, devices } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

// rosshield Web Console — Playwright E2E config (C4 scaffold).
//
// 설계:
// - globalSetup: rosshield-server를 별도 dataDir로 build/seed admin/seed demo한 뒤
//   백그라운드 부팅 → :PORT (기본 8123) 헬스체크 → process.env.E2E_BACKEND_URL 노출.
// - globalTeardown: SIGTERM 후 dataDir 정리.
// - 테스트는 /api proxy를 거치지 않고 direct backend URL 사용 (vite dev 미경유).
//   웹 앱이 동일 origin에서 / + /api/v1을 둘 다 서빙하므로 baseURL = backend URL.
// - 1 worker, 단일 dataDir로 격리 + retry 0 (CI 의도적 보수).
//
// 신규 외부 dep: @playwright/test (1개).

const BACKEND_PORT = Number(process.env.E2E_BACKEND_PORT ?? '8123')
const BASE_URL = process.env.E2E_BACKEND_URL ?? `http://127.0.0.1:${BACKEND_PORT}`

export default defineConfig({
  testDir: './playwright/tests',
  outputDir: './playwright/test-results',
  fullyParallel: false, // 단일 sqlite dataDir이므로 동시 실행 금지.
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI
    ? [['html', { outputFolder: 'playwright/playwright-report', open: 'never' }], ['list']]
    : [['list']],
  timeout: 30_000,
  expect: { timeout: 5_000 },

  globalSetup: path.resolve(__dirname, './playwright/global-setup.ts'),
  globalTeardown: path.resolve(__dirname, './playwright/global-teardown.ts'),

  use: {
    baseURL: BASE_URL,
    // 한국어를 1차 언어로 강제 — i18n store의 detectLocale()이 navigator.language에 의존,
    // CI Linux 기본은 en-US이므로 ko 라벨 검증을 위해 명시.
    locale: 'ko-KR',
    timezoneId: 'Asia/Seoul',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
