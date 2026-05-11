//go:build !linux

// store_other.go — Linux 외 환경(Windows/macOS) Stub.
//
// 정책 (E34, R41-1=B): 본 어댑터는 Linux + /dev/tpm* 환경에서만 동작합니다.
// Windows·macOS에서 `--keystore=tpm`을 시도하면 부팅 실패 — 조용히 file로
// fallback하면 디스크 평문 키 위협이 그대로 노출되므로 명시적 에러로 차단합니다
// (원칙 §11 "단일 바이너리, 다중 껍질" + 보안 일관성).
package tpm

import (
	"crypto/ed25519"
	"sync"
)

// Store는 Linux 외 환경의 stub Store입니다.
// 모든 호출이 ErrTpmDeviceNotAvailable를 반환합니다.
type Store struct {
	mu     sync.Mutex
	closed bool
}

// New는 항상 ErrTpmDeviceNotAvailable를 반환합니다 (Windows/macOS는 실 TPM 미지원).
func New(opts Options) (*Store, error) {
	if opts.SealingDir == "" {
		return nil, ErrSealingDirRequired
	}
	return nil, ErrTpmDeviceNotAvailable
}

// LoadOrCreatePrivateKey는 항상 ErrTpmDeviceNotAvailable를 반환합니다.
//
// 정상 경로에서는 New가 이미 에러를 반환하므로 본 메서드 호출은 도달하지 않지만,
// nil-receiver 보호와 KeyStore interface 만족을 위해 정의합니다.
func (s *Store) LoadOrCreatePrivateKey(handle string) (ed25519.PrivateKey, error) {
	return nil, ErrTpmDeviceNotAvailable
}

// Close는 no-op입니다 (Linux 외에서는 TPM session 미오픈).
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
