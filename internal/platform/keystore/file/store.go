// Package file는 KeyStore의 디스크 파일 어댑터입니다 (E34 Stage 1).
//
// 현재 signer/soft.LoadOrCreatePrivateKey와 동일 동작 — backward compat 유지.
// 두 layer가 같은 코드를 호출하므로 동작 차이 0.
package file

import (
	"crypto/ed25519"

	"github.com/ssabro/rosshield/internal/platform/signer/soft"
)

// Store는 file 기반 KeyStore 구현입니다.
//
// handle = 디스크 path (예: "/var/lib/rosshield/keys/platform.ed25519").
// 파일은 0600, 부모 디렉토리는 0700 권한으로 생성 — soft.LoadOrCreatePrivateKey 위임.
type Store struct{}

// New는 새 file Store 인스턴스를 반환합니다.
func New() *Store {
	return &Store{}
}

// LoadOrCreatePrivateKey는 path에서 ed25519 private key를 로드 또는 생성합니다.
// soft.LoadOrCreatePrivateKey를 그대로 위임 — 동작 차이 0.
func (s *Store) LoadOrCreatePrivateKey(path string) (ed25519.PrivateKey, error) {
	return soft.LoadOrCreatePrivateKey(path)
}

// Close는 no-op입니다 (file 어댑터는 영속 리소스 없음).
func (s *Store) Close() error {
	return nil
}
