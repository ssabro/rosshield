import { expect, test } from '@playwright/test'

import { KO_LABELS } from '../fixtures'
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
  // dict.ts ko: 'compliance.profile.framework' = '프레임워크' (영어 'Framework'에서 한글로 변경)
  await page
    .getByRole('combobox', { name: KO_LABELS.compliance.frameworkLabel, exact: true })
    .click()
  await page.getByRole('option', { name: 'ISMS-P' }).click()

  await page.getByLabel(KO_LABELS.compliance.frameworkVersionLabel).fill('2024')
  await page.getByRole('button', { name: KO_LABELS.compliance.profileAdd }).click()

  // 프로필 테이블에 isms-p 행이 등장.
  // framework 컬럼은 raw value 노출 ("isms-p").
  await expect(page.getByRole('cell', { name: 'isms-p' })).toBeVisible({ timeout: 10_000 })
  await expect(page.getByRole('cell', { name: '2024', exact: true })).toBeVisible()
})
