import { expect, test } from '@playwright/test'

import { loginAsAdmin, resetClientState } from '../helpers'

// compliance.spec — ISMS-P 프로필 추가 → 테이블 노출.
//
// 시드된 session으로 snapshot 생성까지 검증하려면 sessionId를 fetch해야 하므로
// 본 smoke에서는 프로필 추가까지만 검증. snapshot 흐름은 별도 후속 spec에서 강화.

test.beforeEach(async ({ page }) => {
  await resetClientState(page)
  await loginAsAdmin(page)
})

test('add ISMS-P profile and see it listed', async ({ page }) => {
  await page.goto('/compliance')

  await expect(page.getByRole('heading', { name: 'Compliance' }).first()).toBeVisible()

  // Framework Select — "ISMS-P" 옵션 선택.
  // dict.ts:
  //   'compliance.profile.framework' = 'Framework'      → combobox 라벨
  //   'compliance.profile.version'   = 'Framework 버전' → 'Framework' 부분일치를 피하려고 exact 사용.
  await page.getByRole('combobox', { name: 'Framework', exact: true }).click()
  await page.getByRole('option', { name: 'ISMS-P' }).click()

  await page.getByLabel('Framework 버전').fill('2024')
  // dict.ts: 'compliance.profile.add' = '프로필 추가'.
  await page.getByRole('button', { name: '프로필 추가' }).click()

  // 프로필 테이블에 isms-p 행이 등장.
  // framework 컬럼은 raw value 노출 ("isms-p").
  await expect(page.getByRole('cell', { name: 'isms-p' })).toBeVisible({ timeout: 10_000 })
  await expect(page.getByRole('cell', { name: '2024', exact: true })).toBeVisible()
})
