//go:build rosshield_enterprise

package crosswitness

// EditionTag는 본 빌드가 enterprise 모드임을 식별합니다 (E31 scaffold).
//
// 실제 cross-witness fold-in 알고리즘 본체는 E32 stage에서 채워집니다 — 본 파일은
// enterprise 빌드 표면이 살아있음을 검증하기 위한 placeholder.
const EditionTag = "enterprise"
