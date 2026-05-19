import { Globe } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { useT } from '@/i18n/t'
import { nextLocale, useLocaleStore } from '@/i18n/store'

import type { Locale } from '@/i18n/dict'

// D-UI-1 Stage 3 — Header 분해 후 재사용 가능 locale toggle (ko ↔ en).
export function LocaleToggle(): React.ReactElement {
  const locale = useLocaleStore((s) => s.locale)
  const setLocale = useLocaleStore((s) => s.setLocale)
  const t = useT()
  return (
    <Button
      variant="ghost"
      size="sm"
      className="h-8 w-8 px-0"
      onClick={() => setLocale(nextLocale(locale))}
      aria-label={t('header.locale.aria', { label: localeLabel(locale) })}
      title={t('header.locale.tooltip', { label: localeLabel(locale) })}
    >
      <Globe className="h-4 w-4" aria-hidden />
    </Button>
  )
}

function localeLabel(locale: Locale): string {
  return locale === 'ko' ? '한국어' : 'English'
}
