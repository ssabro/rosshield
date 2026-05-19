import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'

import { loginAsAdmin, applyThemeMode, resetClientState } from '../helpers'

// color-contrast.spec — Stage 5b carryover C5-1 회수.
//
// 배경:
//   Stage 5에서 vitest-axe + jsdom 기반 5 페이지 a11y scan을 마쳤으나, jsdom은
//   computed style을 정확히 측정하지 못해 axe-core의 color-contrast rule을 비활성으로
//   유지했다 (false negative "Element has insufficient color contrast of 0:1").
//   참조: docs/design/notes/ui-stage5-polish-report.md §1.3.
//
// 본 spec은 실 chromium 브라우저에서 5 페이지 × light/dark 2 모드 = 10 케이스의
// color-contrast 실측을 수행한다. WCAG 2.2 AA — 일반 텍스트 4.5:1, 대형 텍스트 3:1.
// rule scope:
//   - withRules(['color-contrast']) — 본 spec은 contrast 단독 검증.
//     (구조적 a11y(role/aria/label/heading)는 vitest-axe 쪽에서 cover.)
//
// 실행 모델:
//   - globalSetup이 rosshield-server 백그라운드 부팅 + admin/demo seed 보장.
//   - loginAsAdmin으로 토큰 획득 후 5 페이지 순회.
//   - 각 페이지마다 applyThemeMode(light)/applyThemeMode(dark) 두 번 scan.
//
// 한계:
//   - dynamic content가 늦게 그려지는 페이지는 networkidle 대기로 안정화.
//   - violation 0이 이상적이나, 존재 시 spec output에 컴포넌트 selector + impact가
//     남아 후속 fix carryover로 추적 가능.
//
// 운영 doc: web/playwright/README.md "color-contrast 실측" 절.

interface RoutePage {
  path: string
  name: string
  // 페이지가 그려졌음을 보장하는 KO 라벨 (heading 또는 핵심 텍스트).
  // axe scan 전 visible 대기로 race condition을 줄인다.
  readyText: RegExp
}

const PAGES: RoutePage[] = [
  { path: '/overview', name: 'overview', readyText: /개요/ },
  { path: '/findings', name: 'findings', readyText: /발견|Findings/ },
  { path: '/scans', name: 'scans', readyText: /스캔|Scans/ },
  { path: '/robots', name: 'robots', readyText: /로봇|Robots/ },
  { path: '/fleets', name: 'fleets', readyText: /플릿|Fleet/ },
]

const MODES: Array<'light' | 'dark'> = ['light', 'dark']

test.beforeEach(async ({ page }) => {
  await resetClientState(page)
  await loginAsAdmin(page)
})

for (const route of PAGES) {
  for (const mode of MODES) {
    test(`color-contrast — ${route.name} (${mode})`, async ({ page }) => {
      await page.goto(route.path)
      await applyThemeMode(page, mode)

      // dynamic content 안정화 — query/skeleton 해소까지 networkidle 대기.
      // 5초 timeout (장애 시 fail-fast).
      await page.waitForLoadState('networkidle', { timeout: 5_000 }).catch(() => {
        // networkidle은 SSE/Polling으로 안 끝날 수 있음 — 그래도 진행.
      })

      // 페이지 ready 마커 visible 보장 (skeleton 해소 신호).
      await expect(page.getByText(route.readyText).first()).toBeVisible({ timeout: 5_000 })

      // axe scan — color-contrast rule만 활성.
      // include는 body 전체. exclude는 없음 (Pretendard subset link 등은 텍스트 없음).
      const results = await new AxeBuilder({ page })
        .withRules(['color-contrast'])
        .analyze()

      // 위반 0 기대. 실패 시 results.violations로 상세 노드를 출력.
      if (results.violations.length > 0) {
        // eslint-disable-next-line no-console
        console.log(
          `[color-contrast/${route.name}/${mode}] violations =`,
          JSON.stringify(
            results.violations.map((v) => ({
              id: v.id,
              impact: v.impact,
              help: v.help,
              nodes: v.nodes.map((n) => ({
                target: n.target,
                failureSummary: n.failureSummary,
                html: n.html.slice(0, 200),
              })),
            })),
            null,
            2,
          ),
        )
      }
      expect(results.violations).toEqual([])
    })
  }
}
