import { expect, test } from '@playwright/test'

import { loginAsAdmin, resetClientState } from '../helpers'

// robots.spec — /robots 페이지의 등록 form 토글 + 테이블 렌더 smoke.
//
// 본 spec은 신규 robot 생성 흐름의 폼 노출/필드 입력만 검증.
// 실제 SSH 자격증명 검증은 도메인 단 — E2E는 UI 흐름만 본다.
// seed demo가 만든 demo-robot-{1,2,3}이 테이블에 등장하는지도 함께 본다.

test.beforeEach(async ({ page }) => {
  await resetClientState(page)
  await loginAsAdmin(page)
})

test('robots page shows seeded robots from seed demo', async ({ page }) => {
  await page.goto('/robots')

  // PageHeader title + 테이블 헤더가 있어야 한다.
  await expect(page.getByRole('heading', { name: '로봇' }).first()).toBeVisible()

  // demo seed가 만든 3 robot 중 적어도 1개는 보여야 한다.
  await expect(page.getByText('demo-robot-1')).toBeVisible({ timeout: 10_000 })
})

test('toggle create form reveals fleet/name/host inputs', async ({ page }) => {
  await page.goto('/robots')

  // "로봇 등록" 버튼 클릭 → 폼이 열림.
  await page.getByRole('button', { name: '로봇 등록' }).click()

  await expect(page.getByRole('heading', { name: '새 로봇 등록' })).toBeVisible()
  // 폼 안 라벨 — "Fleet ID"(form)와 "Fleet ID 필터"(filter)가 둘 다 있어 exact 매칭으로
  // form 쪽만 잡는다.
  await expect(page.getByLabel('Fleet ID', { exact: true })).toBeVisible()
  // "이름"·"호스트"는 폼에서 한 곳뿐.
  await expect(page.getByLabel('이름')).toBeVisible()
  await expect(page.getByLabel('호스트')).toBeVisible()

  // 다시 "폼 닫기" 버튼이 노출됨.
  await expect(page.getByRole('button', { name: '폼 닫기' })).toBeVisible()
})
