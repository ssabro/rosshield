import { expect, type Page } from '@playwright/test'

import { E2E_ADMIN, KO_LABELS } from './fixtures'

// 공통 helper — 시나리오마다 반복되는 흐름을 한 곳에 둔다.

// loginAsAdmin은 /login → 폼 작성 → /overview 진입까지를 수행한다.
// 호출자는 로그인 후 추가 액션만 수행하면 된다.
export async function loginAsAdmin(page: Page): Promise<void> {
  await page.goto('/login')
  // login 페이지 카드 타이틀 확인 (i18n 키: login.title).
  await expect(page.getByText(KO_LABELS.login.title, { exact: false })).toBeVisible()

  await page.getByLabel(KO_LABELS.login.email).fill(E2E_ADMIN.email)
  await page.getByLabel(KO_LABELS.login.password).fill(E2E_ADMIN.password)
  await page.getByRole('button', { name: KO_LABELS.login.submit }).click()

  // login.tsx는 /robots로 navigate하지만, _authenticated 가드가 token 후
  // index.tsx redirect로 /overview로 갈 수도 있음 — 단순히 /login 이탈만 확인.
  await page.waitForURL((url) => !url.pathname.endsWith('/login'), { timeout: 10_000 })
}

// resetClientState는 localStorage·sessionStorage·cookies를 모두 비운다.
// 각 spec의 깨끗한 시작을 보장 (i18n 토글·테마·auth 잔여 상태 격리).
export async function resetClientState(page: Page): Promise<void> {
  // baseURL로 한 번 진입해야 same-origin localStorage에 접근 가능.
  await page.goto('/')
  await page.evaluate(() => {
    try {
      window.localStorage.clear()
      window.sessionStorage.clear()
    } catch {
      // ignore — 일부 브라우저는 unsupported
    }
  })
  await page.context().clearCookies()
}
