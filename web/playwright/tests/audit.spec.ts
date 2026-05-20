import { expect, test } from '@playwright/test'

import { loginAsAdmin, resetClientState } from '../helpers'

// audit.spec — /audit 페이지에서 ChainHead의 seq + hash가 렌더링되는지 검증.
//
// seed admin/seed demo 후이므로 audit 엔트리가 ≥1개 존재 (audit head seq > 0).

test.beforeEach(async ({ page }) => {
  await resetClientState(page)
  await loginAsAdmin(page)
})

test('audit page renders chain head seq + hash', async ({ page }) => {
  await page.goto('/audit')

  await expect(page.getByRole('heading', { name: '감사' }).first()).toBeVisible()
  // exact: true — description에 "chain head" substring이 있어 strict mode violation 회피
  await expect(page.getByText('Chain Head', { exact: true })).toBeVisible()

  // Sequence 라벨이 있고 옆에 숫자가 표시된다.
  // dict.ts: 'audit.head.seq': 'Sequence'
  await expect(page.getByText('Sequence')).toBeVisible()

  // hash 라벨도 존재 — sha256은 16진 64자로 노출.
  await expect(page.getByText('Hash (sha256)')).toBeVisible()

  // mono 영역에서 hash 형태(sha256 hex 64자) 또는 숫자(seq)가 보이는지.
  // 값 자체는 매 실행 다르므로 정규식 매칭으로 확인.
  const monoTexts = await page.locator('span.font-mono').allInnerTexts()
  const hasSeqLikeNumber = monoTexts.some((t) => /^\d+$/.test(t.trim()))
  expect(hasSeqLikeNumber).toBe(true)
})
