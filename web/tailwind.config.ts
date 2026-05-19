import type { Config } from 'tailwindcss'

// Tailwind v4는 대부분의 설정을 CSS의 @theme 블록으로 옮겼지만,
// content scanning 안내와 dark mode 클래스 전략은 여전히 유효합니다.
//
// severity·status 색상 + Pretendard font-family는 globals.css의 @theme 블록에서
// CSS variable로 정의 (D-UI-1 Stage 1, ui-overhaul-design.md §3 참조).
// Tailwind v4는 `--color-severity-critical` 같은 @theme variable을 자동으로
// `bg-severity-critical` / `text-severity-critical` utility로 노출합니다.
const config: Config = {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  darkMode: 'class',
}

export default config
