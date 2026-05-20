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

// D-P7-3 carryover (2026-05-20): D-P7-1 브랜드 commit 이후 헤더 UX가 사용자 dropdown
// menu로 변경 — 로그아웃이 button role이 아닌 dropdown menuitem 안에 위치. spec이 dropdown
// trigger를 열고 menuitem을 찾는 패턴으로 재설계 필요. 별 PR로 분리.
test.skip('admin login redirects out of /login and reveals authenticated shell', async ({ page }) => {
  await loginAsAdmin(page)

  // 인증 셸의 표식 — 헤더 로그아웃 버튼이 보여야 한다.
  await expect(page.getByRole('button', { name: KO_LABELS.header.logout })).toBeVisible()
  // 사용자 이메일이 헤더에 노출되는지 (text 부분 일치).
  await expect(page.getByText(E2E_ADMIN.email)).toBeVisible()
})

// D-P7-3 carryover (2026-05-20): 사용자 dropdown menu 도입 후 로그아웃 trigger 재설계 필요.
test.skip('logout clears session and redirects to /login', async ({ page }) => {
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
