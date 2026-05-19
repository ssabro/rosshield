import { Monitor, Moon, Sun } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { useT } from '@/i18n/t'
import { useThemeStore, type Theme } from '@/stores/theme'

import type { DictKey } from '@/i18n/dict'

// D-UI-1 Stage 3 — Header 분해 후 재사용 가능 theme toggle.
// 토글 순서: light → dark → system → light (Header.test.tsx와 동일).

export function ThemeToggle(): React.ReactElement {
  const theme = useThemeStore((s) => s.theme)
  const setTheme = useThemeStore((s) => s.setTheme)
  const t = useT()
  const themeLbl = t(themeLabelKey(theme))
  return (
    <Button
      variant="ghost"
      size="sm"
      className="h-8 w-8 px-0"
      onClick={() => setTheme(nextTheme(theme))}
      aria-label={t('header.theme.aria', { label: themeLbl })}
      title={t('header.theme.tooltip', { label: themeLbl })}
    >
      <ThemeIcon theme={theme} />
    </Button>
  )
}

// nextTheme — 토글 순서: light → dark → system → light
// Header.test.tsx 회귀 호환을 위해 같은 시그니처를 유지하고 Header.tsx에서 re-export.
export function nextTheme(theme: Theme): Theme {
  if (theme === 'light') return 'dark'
  if (theme === 'dark') return 'system'
  return 'light'
}

export function themeLabelKey(theme: Theme): DictKey {
  if (theme === 'light') return 'header.theme.light'
  if (theme === 'dark') return 'header.theme.dark'
  return 'header.theme.system'
}

function ThemeIcon({ theme }: { theme: Theme }): React.ReactElement {
  if (theme === 'light') return <Sun className="h-4 w-4" aria-hidden />
  if (theme === 'dark') return <Moon className="h-4 w-4" aria-hidden />
  return <Monitor className="h-4 w-4" aria-hidden />
}
