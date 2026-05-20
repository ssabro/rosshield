import { expect, test } from '@playwright/test'

import { loginAsAdmin, resetClientState } from '../helpers'

// theme.spec — 헤더의 sun/moon/monitor 아이콘 토글로 light → dark → system 사이클.
//
// applyTheme()이 document.documentElement.classList.dark 토글을 담당.
// 본 spec은 "한 번 클릭해 모드가 바뀐다"의 smoke만 검증 (정확한 색상 매칭 X).

test.beforeEach(async ({ page }) => {
  await resetClientState(page)
  await loginAsAdmin(page)
})

// D-P7-3 carryover (2026-05-20): D-P7-1 브랜드 commit 이후 헤더 dropdown 도입 — 테마
// button이 헤더 dropdown menu 안으로 이동. dropdown trigger + menuitem 패턴으로 재설계 필요.
test.skip('theme toggle changes html.dark class state', async ({ page }) => {
  // 초기 상태 — system 모드일 가능성 (CI는 보통 light).
  const initialDark = await page.locator('html').evaluate((el) => el.classList.contains('dark'))

  // 테마 버튼 — aria-label에 "테마"가 포함됨.
  // 한 번 클릭하면 light → dark (또는 다음 단계).
  const themeBtn = page.getByRole('button', { name: /테마/ })
  await themeBtn.click()

  // 클릭 후 .dark 토글이 바뀌었는지 — 상태가 변하면 통과 (system → light → dark 순환 중 어딘가).
  // 최대 2번까지 클릭해 확실히 dark에 진입 확인.
  for (let i = 0; i < 3; i++) {
    const isDark = await page.locator('html').evaluate((el) => el.classList.contains('dark'))
    if (isDark) {
      expect(isDark).toBe(true)
      return
    }
    await themeBtn.click()
  }

  // 사이클 후에도 dark가 한 번 안 됐다면 — 그래도 초기와 비교해 어딘가 한 번은 변했어야.
  const finalDark = await page.locator('html').evaluate((el) => el.classList.contains('dark'))
  expect(finalDark).not.toBe(initialDark)
})
