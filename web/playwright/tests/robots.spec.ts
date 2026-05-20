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

  // D-P7-1: '로봇 등록' button 클릭 → Dialog 모달 열림 (종전 inline 폼에서 마이그레이션).
  await page.getByRole('button', { name: '로봇 등록' }).click()

  await expect(page.getByRole('heading', { name: '새 로봇 등록' })).toBeVisible()
  // Dialog 안 폼 라벨 — dict ko: 'robots.form.fleet'='플릿 ID', filter는 '플릿 ID 필터'.
  // exact 매칭으로 form 쪽만 잡는다.
  await expect(page.getByLabel('플릿 ID', { exact: true })).toBeVisible()
  await expect(page.getByLabel('이름')).toBeVisible()
  await expect(page.getByLabel('호스트')).toBeVisible()
  // '폼 닫기' 버튼은 Dialog 마이그레이션 후 삭제 — ESC/X 아이콘이 dialog close.
})
