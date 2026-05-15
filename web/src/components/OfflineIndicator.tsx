// PWA Stage 3 — 오프라인 상태 banner (design doc §6.5 + §7 Stage 3).
//
// `useIsOffline` hook을 구독해 오프라인 상태가 되면 상단 fixed banner로 사용자에게
// 안내합니다. 메시지는 i18n 키 `pwa.offline.banner` (ko/en).
//
// 스타일 (design doc §6.5 Tailwind banner 권장):
//   - top fixed, 전체 폭, z-50으로 dialog/popover 아래에 위치.
//   - destructive 톤(amber/orange) 으로 가시성 확보 — 단, 너무 강한 red는 피해
//     "현재 사용은 계속 가능" 메시지 톤 유지.
//   - role="status" + aria-live="polite" — 스크린 리더 자연 안내.

import { useT } from '@/i18n/t'
import { useIsOffline } from '@/lib/use-is-offline'

export function OfflineIndicator(): React.ReactElement | null {
  const offline = useIsOffline()
  const t = useT()

  if (!offline) {
    return null
  }

  return (
    <div
      role="status"
      aria-live="polite"
      data-testid="offline-indicator"
      className="fixed inset-x-0 top-0 z-50 flex items-center justify-center gap-2 border-b border-amber-500/40 bg-amber-100 px-4 py-2 text-xs font-medium text-amber-900 shadow-sm dark:border-amber-400/40 dark:bg-amber-950 dark:text-amber-100"
    >
      <span aria-hidden className="inline-block h-2 w-2 rounded-full bg-amber-500" />
      <span>{t('pwa.offline.banner')}</span>
    </div>
  )
}
