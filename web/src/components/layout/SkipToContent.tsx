import { useT } from '@/i18n/t'

// D-UI-1 Stage 3 — Skip-to-content link.
//
// 키보드 사용자/스크린리더가 매 페이지에서 sidebar+header를 건너뛰고 본문으로
// 이동할 수 있게 해주는 P0 a11y 요구. KWCAG·WCAG 2.4.1 (Bypass Blocks).
//   - sr-only로 평소엔 보이지 않고, Tab으로 포커스가 잡히면 좌상단에 노출.
//   - target은 `#main-content` (route shell의 <main id="main-content">).
//   - href "#main-content"는 hash 점프 — TanStack Router는 hash만 변경 시
//     리렌더 없이 브라우저 native anchor 동작. tabIndex={-1}로 main에 focus 이동.
//
// 한·영 라벨은 i18n 사전 키 `a11y.skipToContent`.
export function SkipToContent(): React.ReactElement {
  const t = useT()
  return (
    <a
      href="#main-content"
      className="sr-only focus:not-sr-only focus:fixed focus:left-2 focus:top-2 focus:z-[100] focus:rounded focus:bg-primary focus:px-4 focus:py-2 focus:text-sm focus:font-medium focus:text-primary-foreground focus:shadow-lg focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2"
    >
      {t('a11y.skipToContent')}
    </a>
  )
}
