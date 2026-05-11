//go:build !linux

package tpm_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/keystore/tpm"
)

// TestNewReturnsErrTpmDeviceNotAvailable — Linux 외 환경(Windows/macOS)에서는
// SealingDir이 정상이어도 TPM 디바이스 부재로 항상 ErrTpmDeviceNotAvailable.
//
// 이 정책은 "조용한 fallback 금지"(원칙 §11) — Windows에서 `--keystore=tpm`을
// 시도하면 명시적 부팅 실패. 운영자가 실수로 보안이 약한 file 어댑터로 떨어지지
// 않도록 보장합니다.
func TestNewReturnsErrTpmDeviceNotAvailable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := tpm.New(tpm.Options{SealingDir: filepath.Join(dir, "keys", "tpm")})
	if !errors.Is(err, tpm.ErrTpmDeviceNotAvailable) {
		t.Errorf("New on non-linux err = %v, want ErrTpmDeviceNotAvailable", err)
	}
}

// TestStoreCloseIsNilSafe — nil receiver Close는 안전해야 합니다 (방어적).
func TestStoreCloseIsNilSafe(t *testing.T) {
	t.Parallel()
	var s *tpm.Store
	if err := s.Close(); err != nil {
		t.Errorf("Close on nil receiver: %v, want nil", err)
	}
}
