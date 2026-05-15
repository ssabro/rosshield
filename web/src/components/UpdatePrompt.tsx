// PWA Stage 3 — 새 SW 갱신 알림 toast (design doc §6.4 + §7 Stage 3).
//
// `usePwaUpdate` hook으로 needRefresh 상태를 구독해 새 버전이 install 완료되면
// 우하단 toast로 사용자에게 reload 안내. 사용자가 reload 버튼을 클릭하면
// `updateServiceWorker(true)` 호출 → skipWaiting + 페이지 reload.
//
// design doc D-PWA-6 (registerType: 'prompt') 결정에 따라 자동 갱신 0 — 사용자가
// 작업 중 데이터 손실 없이 명시 동의하에 갱신.
//
// 스타일:
//   - 우하단 fixed toast (z-50), 좁은 max-w로 작업 가시성 우선.
//   - role="alert" — 신규 갱신은 적극 알림 톤.

import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { useT } from '@/i18n/t'
import { usePwaUpdate } from '@/lib/pwa-update'

export function UpdatePrompt(): React.ReactElement | null {
  const t = useT()
  const { needRefresh, reload, dismissNeedRefresh } = usePwaUpdate()
  const [reloading, setReloading] = useState<boolean>(false)

  if (!needRefresh) {
    return null
  }

  const onReload = async (): Promise<void> => {
    setReloading(true)
    try {
      await reload()
      // reload 호출 후 페이지가 곧 새로고침되므로 finally의 setReloading은
      // 실제 도달하지 않을 수 있음 — 안전하게 catch만.
    } catch (err) {
      // eslint-disable-next-line no-console
      console.warn('[UpdatePrompt] reload failed:', err)
      setReloading(false)
    }
  }

  return (
    <div
      role="alert"
      data-testid="update-prompt"
      className="fixed bottom-4 right-4 z-50 flex max-w-sm items-center gap-3 rounded-lg border border-border bg-card px-4 py-3 text-sm shadow-lg"
    >
      <span className="flex-1 text-foreground">{t('pwa.update.available')}</span>
      <Button
        size="sm"
        onClick={() => void onReload()}
        disabled={reloading}
        data-testid="update-prompt-reload"
      >
        {t('pwa.update.reload')}
      </Button>
      <Button
        size="sm"
        variant="ghost"
        onClick={dismissNeedRefresh}
        aria-label="dismiss"
        data-testid="update-prompt-dismiss"
      >
        ×
      </Button>
    </div>
  )
}
