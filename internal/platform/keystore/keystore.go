// Package keystore implements E34 — KeyStore 추상화.
//
// 본 패키지는 ed25519 private key 영속의 driver-agnostic 인터페이스를 정의합니다.
// bootstrap이 KeyStore를 통해 키를 로드 → soft.WrapPrivateKey로 signer.Signer 생성.
//
// 어댑터:
//   - file (`internal/platform/keystore/file/`) — 디스크 파일에 raw ed25519 private key 저장 (현재 동작 호환)
//   - tpm  (`internal/platform/keystore/tpm/`)  — TPM 2.0 PCR-sealed (Stage 2+ 본격 구현)
//
// 도메인 import 가드: 본 패키지는 internal/domain/* 를 import하지 않습니다.
//
// 설계: docs/design/notes/e34-tpm-design.md (R40-2 = swtpm 결정 + R41 KeyStore 모델·라이브러리 결정 후속).
package keystore

import (
	"crypto/ed25519"
	"errors"
)

// KeyStore는 ed25519 private key 영속·로드의 driver-agnostic 추상입니다.
//
// 어댑터별 handle 의미:
//   - file: 디스크 path (예: "/var/lib/rosshield/keys/platform.ed25519")
//   - tpm:  TPM object name 또는 NVRAM index (어댑터별 컨벤션)
type KeyStore interface {
	// LoadOrCreatePrivateKey는 handle에서 private key를 로드 또는 새로 생성·영속화합니다.
	// 두 번째 호출부터는 같은 키를 반환해야 합니다 (bootstrap 재시작 시 audit checkpoint
	// 검증을 위해 필수).
	LoadOrCreatePrivateKey(handle string) (ed25519.PrivateKey, error)

	// Close는 어댑터별 리소스(TPM session, HSM session 등)를 해제합니다.
	// file 어댑터는 no-op.
	Close() error
}

// 공통 에러.
var (
	// ErrKeyMismatch는 같은 handle에 대해 두 번째 호출이 첫 호출과 다른 키를 반환할 때 사용됩니다.
	// (정상 어댑터에서는 발생 안 함 — 디버그/테스트용)
	ErrKeyMismatch = errors.New("keystore: loaded key differs from previously created key")

	// ErrUnsupportedDriver는 알 수 없는 driver 이름이 build* 호출에 전달될 때 사용됩니다.
	ErrUnsupportedDriver = errors.New("keystore: unsupported driver")
)
