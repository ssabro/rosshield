// Package tpm은 KeyStore의 TPM 2.0 PCR-sealed 어댑터입니다 (E34).
//
// **Stage 1 placeholder** — 실 TPM 구현은 R41 결정(KeyStore 모델·Go TPM 라이브러리·
// PCR set) 후 Stage 2~4에서 채워집니다. 본 stage는 keystore interface scaffold만
// 제공 — bootstrap에서 `--keystore=tpm` flag 인지하여 ErrTpmNotImplemented 반환,
// 사용자에게 명시적 실패 (조용한 fallback 금지, 원칙 §11).
//
// R40-2 결정 (2026-05-11): TPM 시뮬레이터 = swtpm (Linux 표준, CI testcontainers 친화).
// R41 결정 후보:
//   - R41-1 KeyStore 모델 — A) TPM Signer 어댑터 / **B) TPM Keystore + soft Signer (권장)** / Hybrid
//   - R41-2 Go TPM 라이브러리 — **google/go-tpm-tools (권장)** / canonical/go-tpm2 / google/go-tpm low-level
//   - R41-3 기본 PCR set — 권장 [0,2,4,7] / 더 strict [0,2,4,7,11,12] / custom
//
// 설계: docs/design/notes/e34-tpm-design.md (sub-agent 작성 중).
package tpm

import (
	"crypto/ed25519"
	"errors"
)

// ErrTpmNotImplemented는 Stage 1 placeholder 상태를 나타냅니다.
// Stage 2~4에서 google/go-tpm-tools(또는 R41-2 결정 라이브러리)로 PCR seal/unseal 채움.
var ErrTpmNotImplemented = errors.New(
	"keystore/tpm: TPM 2.0 sealing not yet implemented (E34 Stage 1 placeholder, see docs/design/notes/e34-tpm-design.md)",
)

// Store는 TPM 2.0 어댑터 placeholder입니다.
//
// 본 stage는 KeyStore interface 만족만 제공 — 실제 TPM 호출은 ErrTpmNotImplemented.
// bootstrap이 `--keystore=tpm`으로 본 store를 결선하면 첫 LoadOrCreatePrivateKey
// 호출에서 즉시 부팅 실패 (의도 — TPM 봉인 미구현 상태에서 조용히 file로 fallback 금지).
type Store struct{}

// New는 placeholder Store를 반환합니다.
//
// Stage 2 이후 시그니처:
//
//	func New(opts Options) (*Store, error)
//
// Options에는 PCR selection, parent handle(EK/SRK), TPM 디바이스 path 등이 추가됨.
func New() *Store {
	return &Store{}
}

// LoadOrCreatePrivateKey는 항상 ErrTpmNotImplemented를 반환합니다 (Stage 1).
func (s *Store) LoadOrCreatePrivateKey(handle string) (ed25519.PrivateKey, error) {
	return nil, ErrTpmNotImplemented
}

// Close는 no-op입니다 (Stage 1은 TPM session 미오픈).
func (s *Store) Close() error {
	return nil
}
