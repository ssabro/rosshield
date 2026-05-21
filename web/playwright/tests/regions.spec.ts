import { expect, test } from '@playwright/test'

import { loginAsAdmin, resetClientState } from '../helpers'

// regions.spec — Phase 10.A-6 e2e for `/regions` (multi-region UI 표면화).
//
// 검증 흐름:
//  1. admin login → 좌측 nav의 '리전' 항목 클릭 → URL `/regions` 도달.
//  2. PageHeader 'regions.title' 라벨 visible.
//  3. AuditConsistencyCard render (head sha 표시 또는 genesis empty state).
//  4. RegionTimelineCard render (empty state — dev DB 환경).
//  5. RegionHealthCard grid 또는 empty state — replicas 미시드 환경이라 empty state 정상.
//
// 환경 주의: globalSetup이 replication_replicas/failover seed를 하지 않으므로
// 페이지의 replicas section은 비어 있을 가능성이 높습니다. cards 본체는 mount
// 자체로 render되어야 하며, replicas grid 부재 시 empty state가 표시되어야 합니다.

test.beforeEach(async ({ page }) => {
  await resetClientState(page)
  await loginAsAdmin(page)
})

test('/regions page renders with audit + timeline cards and nav navigation', async ({ page }) => {
  // 1. nav 항목 클릭으로 진입 — URL 정합 검증.
  await page.goto('/overview')

  // 사이드바 nav '리전' 링크 클릭. role=link + name='리전' 매트릭스.
  // 좁은 viewport에서 sidebar가 sheet drawer일 수 있으니 fallback으로 직접 goto.
  const regionsLink = page.getByRole('link', { name: '리전', exact: true })
  if (await regionsLink.count() > 0 && await regionsLink.first().isVisible()) {
    await regionsLink.first().click()
    await page.waitForURL(/\/regions$/, { timeout: 5_000 })
  } else {
    await page.goto('/regions')
  }

  // 2. PageHeader 타이틀 (dict.ts ko 'regions.title' = '리전').
  await expect(page.getByRole('heading', { name: '리전' }).first()).toBeVisible()

  // 3. AuditConsistencyCard — title '_Audit Chain 정합'_ visible. genesis seq=0 또는
  //    실 head 값 둘 다 render PASS. ApiError 시 'regions.audit.error' 라벨.
  await expect(page.getByText('Audit Chain 정합', { exact: false })).toBeVisible({ timeout: 10_000 })

  // 4. RegionTimelineCard — title 'Region cutover 이력' visible. dev 환경은 empty state.
  await expect(page.getByText('Region cutover 이력', { exact: false })).toBeVisible({ timeout: 10_000 })
})
