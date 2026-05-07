import { DICTS, type DictKey, type Locale } from './dict'
import { useLocaleStore } from './store'

// translate — locale + key → 번역된 문자열.
// vars로 {placeholder} 보간을 지원한다 (선택). missing key는 `[missing:key]` 표시.

export function translate(
  locale: Locale,
  key: DictKey,
  vars?: Record<string, string | number>,
): string {
  const dict = DICTS[locale]
  let value = dict[key]
  if (typeof value !== 'string') {
    return `[missing:${key}]`
  }
  if (vars) {
    for (const [name, v] of Object.entries(vars)) {
      value = value.replaceAll(`{${name}}`, String(v))
    }
  }
  return value
}

// useT — React hook. 컴포넌트 내에서 t('key') 호출.
// store가 변하면 컴포넌트 재렌더 → 새 locale의 번역값을 반환한다.
export function useT(): (
  key: DictKey,
  vars?: Record<string, string | number>,
) => string {
  const locale = useLocaleStore((s) => s.locale)
  return (key, vars) => translate(locale, key, vars)
}
