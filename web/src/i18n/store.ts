import { create } from 'zustand'
import { persist } from 'zustand/middleware'

import type { Locale } from './dict'

// 언어 store — locale을 localStorage에 영속.
// 초기값은 navigator.language 추정 (ko로 시작하면 'ko', 그 외 'en').

interface LocaleState {
  locale: Locale
  setLocale: (l: Locale) => void
}

function detectLocale(): Locale {
  if (typeof navigator === 'undefined') return 'ko'
  const lang = navigator.language?.toLowerCase() ?? ''
  return lang.startsWith('ko') ? 'ko' : 'en'
}

export const useLocaleStore = create<LocaleState>()(
  persist(
    (set) => ({
      locale: detectLocale(),
      setLocale: (locale) => set({ locale }),
    }),
    { name: 'rosshield-locale' },
  ),
)

export function nextLocale(locale: Locale): Locale {
  return locale === 'ko' ? 'en' : 'ko'
}
