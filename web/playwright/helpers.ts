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

// applyThemeMode는 zustand persist 형식과 동일하게 localStorage 'rosshield-theme' 키를
// 세팅하고 즉시 document.documentElement.classList의 'dark' 토글까지 적용한다.
//
// 사용 예:
//   await applyThemeMode(page, 'dark')  // → html.dark 클래스 추가
//   await applyThemeMode(page, 'light') // → html.dark 클래스 제거
//
// theme.ts의 applyTheme()을 직접 호출하지 않고 DOM 토글로 끝내는 이유:
//   - color-contrast 측정만 목적이므로 zustand store rehydrate를 기다릴 필요 없음.
//   - light/dark 두 모드 모두에서 globals.css의 :root / .dark CSS variable이 적용됨.
//
// 호출 시점: page.goto() 후 (localStorage same-origin 접근 가능 시점).
export async function applyThemeMode(page: Page, mode: 'light' | 'dark'): Promise<void> {
  await page.evaluate((m) => {
    try {
      window.localStorage.setItem('rosshield-theme', JSON.stringify({ state: { theme: m }, version: 0 }))
    } catch {
      // ignore — persist storage 실패해도 DOM 토글은 진행
    }
    document.documentElement.classList.toggle('dark', m === 'dark')
  }, mode)
}
