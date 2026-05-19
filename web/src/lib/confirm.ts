// D-UI-1 Stage 2 — `confirm()` imperative re-export.
//
// 호출지(`@/lib/confirm`)와 구현지(`@/components/common/ConfirmDialog`)를 분리해
// 도메인 코드가 components 트리에 직접 결합하지 않도록 한다. Host 컴포넌트는
// 그대로 `components/common`에서 가져와 App.tsx에서 마운트.

export { confirm, type ConfirmOptions } from '@/components/common/ConfirmDialog'
