// D-UI-1 Stage 5 — vitest-axe configuration.
//
// axe-core 기반 WCAG 2.2 AA + KWCAG 자동 검증 helper.
// jsdom 환경에서 일부 rule(color-contrast 등)은 computed style을 정확히
// 측정하기 어려우므로 enable/disable 매트릭스를 본 파일에서 한 곳에 정의.
//
// 사용:
//   import { axe } from '@/test/axe'
//   const { container } = render(<MyComp />)
//   const results = await axe(container)
//   expect(results).toHaveNoViolations()
//
// 페이지 specific 예외는 호출부에서 `additionalOptions`로 override.
import { configureAxe } from 'vitest-axe'

// jsdom 한계 — color-contrast는 실제 렌더 픽셀이 필요하므로 jsdom에서는
// false negative 가 잦음. CI 환경에서 일관성 위해 disable.
// 색상 대비는 ui-review-a11y-security.md §3에서 디자인 타임에 검증 (Stage 1 token).
//
// 이외 rule 은 axe-core 기본 (WCAG 2.0/2.1/2.2 A+AA) 그대로.
export const axe = configureAxe({
  rules: {
    'color-contrast': { enabled: false },
    // landmark/region rule 은 페이지 컴포넌트만 별도 렌더 시 false positive 가능
    // (실제 App.tsx 에는 main landmark 있음). 컴포넌트 단위 axe scan 에서는 region 비활성.
    region: { enabled: false },
    'landmark-one-main': { enabled: false },
    'page-has-heading-one': { enabled: false },
  },
})
