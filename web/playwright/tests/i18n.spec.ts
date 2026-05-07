import { expect, test } from '@playwright/test'

import { EN_LABELS, KO_LABELS } from '../fixtures'
import { loginAsAdmin, resetClientState } from '../helpers'

// i18n.spec — 헤더 Globe 토글로 ko ↔ en 전환 smoke.
//
// dict는 'header.locale.tooltip' aria-label에 현재 라벨을 노출하므로
// 토글 후 sidebar 메뉴 라벨로 검증.

test.beforeEach(async ({ page }) => {
  await resetClientState(page)
  await loginAsAdmin(page)
})

test('language toggle switches ko nav labels to en', async ({ page }) => {
  // ko 기본 — Sidebar에 "개요" 노출.
  await expect(page.getByRole('link', { name: KO_LABELS.nav.overview })).toBeVisible()

  // 헤더 Globe 버튼 클릭 (aria-label에 "언어"가 포함됨).
  await page.getByRole('button', { name: /언어/ }).click()

  // 토글 후 영어로 전환 — Sidebar "Overview" 노출.
  await expect(page.getByRole('link', { name: EN_LABELS.nav.overview })).toBeVisible({
    timeout: 5_000,
  })

  // 헤더 로그아웃 라벨도 영어로.
  await expect(page.getByRole('button', { name: EN_LABELS.header.logout })).toBeVisible()
})
