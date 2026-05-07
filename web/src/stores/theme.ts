import { create } from 'zustand'
import { persist } from 'zustand/middleware'

// 테마 모드 — light/dark/system. 기본 system(prefers-color-scheme).
// applyTheme()이 document.documentElement.classList의 'dark' 토글을 담당하고,
// CSS 변수(:root vs .dark)가 실제 색을 변경한다.

export type Theme = 'light' | 'dark' | 'system'

interface ThemeState {
  theme: Theme
  setTheme: (t: Theme) => void
}

export const useThemeStore = create<ThemeState>()(
  persist(
    (set) => ({
      theme: 'system',
      setTheme: (theme) => set({ theme }),
    }),
    { name: 'rosshield-theme' },
  ),
)

export function resolveDark(theme: Theme): boolean {
  if (theme === 'dark') return true
  if (theme === 'light') return false
  return (
    typeof window !== 'undefined' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches
  )
}

export function applyTheme(theme: Theme): void {
  if (typeof document === 'undefined') return
  document.documentElement.classList.toggle('dark', resolveDark(theme))
}
