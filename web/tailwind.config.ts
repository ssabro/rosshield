import type { Config } from 'tailwindcss'

// Tailwind v4는 대부분의 설정을 CSS의 @theme 블록으로 옮겼지만,
// content scanning 안내와 dark mode 클래스 전략은 여전히 유효합니다.
const config: Config = {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  darkMode: 'class',
}

export default config
