import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import './styles/globals.css'
import { registerServiceWorker } from './lib/pwa-register'

const rootEl = document.getElementById('root')
if (!rootEl) {
  throw new Error('#root element not found in index.html')
}

createRoot(rootEl).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

// PWA Stage 2/3 — service worker 등록 + 갱신 알림 결선 (design doc D-PWA-1
// generateSW + D-PWA-6 prompt). registerServiceWorker는 onNeedRefresh /
// onOfflineReady 콜백을 module-level state에 기록하고, _authenticated/route.tsx의
// `OfflineIndicator` + `UpdatePrompt` 컴포넌트가 `usePwaUpdate` hook으로 구독합니다.
registerServiceWorker()
