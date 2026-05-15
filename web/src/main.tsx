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

// PWA Stage 2 — service worker 등록 (design doc D-PWA-1 generateSW + D-PWA-6 prompt).
// 갱신 알림 UX(`useRegisterSW` hook 결선)는 Stage 3에서 추가됩니다.
registerServiceWorker()
