// Package signer는 detached signature 생성·검증의 공개 표면을 정의합니다.
//
// Phase 1은 메모리 보관 Ed25519 키(`soft`)만 제공합니다. 파일 영속·키 회전·HSM/TPM
// 어댑터는 후속 에픽(E2 audit checkpoint·Phase 3 SKU)에서 추가됩니다.
//
// 표면 설계 근거:
//   - audit 체인 checkpoint(§10.5)·report PDF 서명(§E8)·pack manifest 검증(§E4)이 모두 동일 인터페이스 사용
//   - keyId는 hash chain·report 메타에 포함되어 검증 시점에 키 식별
package signer

import "errors"

// Signer는 detached signature를 생성·검증합니다.
type Signer interface {
	// Sign은 payload에 대한 서명과 사용된 키 ID를 반환합니다.
	Sign(payload []byte) (sig []byte, keyID string, err error)

	// Verify는 payload와 sig가 일치하는지 검증합니다. 실패 시 ErrInvalidSignature.
	Verify(payload, sig []byte) error

	// PublicKey는 검증에 사용할 공개키 raw bytes를 반환합니다.
	PublicKey() []byte

	// KeyID는 현재 활성 키의 식별자를 반환합니다.
	// 형식: "key_<16-hex>" (sha256(publicKey)[:8] hex). 키마다 안정적.
	KeyID() string
}

// 공통 에러.
var (
	ErrInvalidSignature = errors.New("signer: invalid signature")
	ErrShortSignature   = errors.New("signer: signature too short")
)
