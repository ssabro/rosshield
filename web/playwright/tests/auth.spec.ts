import { expect, test } from '@playwright/test'

import { E2E_ADMIN, KO_LABELS } from '../fixtures'
import { loginAsAdmin, resetClientState } from '../helpers'

// auth.spec — 로그인 → 보호 라우트 진입 → 로그아웃 → /login 복귀.
//
// 검증 포인트:
//   1. /login 폼에 admin 자격증명을 넣으면 보호 라우트로 진입.
//   2. 로그아웃 버튼이 헤더에 있고 누르면 /login으로 돌아온다.
//   3. localStorage의 zustand persist에서 token이 비워진다.

test.beforeEach(async ({ page }) => {
  await resetClientState(page)
})

test('admin login redirects out of /login and reveals authenticated shell', async ({ page }) => {
  await loginAsAdmin(page)

  // 인증 셸의 표식 — 헤더 로그아웃 버튼이 보여야 한다.
  await expect(page.getByRole('button', { name: KO_LABELS.header.logout })).toBeVisible()
  // 사용자 이메일이 헤더에 노출되는지 (text 부분 일치).
  await expect(page.getByText(E2E_ADMIN.email)).toBeVisible()
})

test('logout clears session and redirects to /login', async ({ page }) => {
  await loginAsAdmin(page)

  await page.getByRole('button', { name: KO_LABELS.header.logout }).click()
  await page.waitForURL((url) => url.pathname.endsWith('/login'), { timeout: 5_000 })

  // /login 카드가 다시 보인다.
  await expect(page.getByText(KO_LABELS.login.title, { exact: false })).toBeVisible()

  // localStorage에 accessToken이 사라졌는지.
  const tokenCleared = await page.evaluate(() => {
    const raw = window.localStorage.getItem('rosshield-auth')
    if (!raw) return true
    try {
      const parsed = JSON.parse(raw)
      const token = parsed?.state?.accessToken
      return token == null || token === ''
    } catch {
      return true
    }
  })
  expect(tokenCleared).toBe(true)
})

test('protected routes redirect unauthenticated requests to /login', async ({ page }) => {
  await page.goto('/overview')
  await page.waitForURL((url) => url.pathname.endsWith('/login'), { timeout: 5_000 })
  await expect(page.getByText(KO_LABELS.login.title, { exact: false })).toBeVisible()
})
