// Package tpm_test — cross-platform 단위 테스트 (Linux/Windows/macOS 공통).
//
// 실제 TPM seal/unseal 검증은 store_linux_test.go의 `//go:build linux && tpm_integration`
// 통합 테스트에서 simulator로 수행합니다.
package tpm_test

import (
	"errors"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/keystore/tpm"
)

// TestErrTpmDeviceNotAvailableSentinel — sentinel 노출 보장 (bootstrap이
// errors.Is로 분기 가능해야 함).
func TestErrTpmDeviceNotAvailableSentinel(t *testing.T) {
	t.Parallel()
	if tpm.ErrTpmDeviceNotAvailable == nil {
		t.Fatal("ErrTpmDeviceNotAvailable is nil — sentinel must be exported")
	}
}

// TestErrPcrMismatchSentinel — sentinel 노출 보장.
func TestErrPcrMismatchSentinel(t *testing.T) {
	t.Parallel()
	if tpm.ErrPcrMismatch == nil {
		t.Fatal("ErrPcrMismatch is nil — sentinel must be exported")
	}
}

// TestErrSealingDirRequiredSentinel — sentinel 노출 보장.
func TestErrSealingDirRequiredSentinel(t *testing.T) {
	t.Parallel()
	if tpm.ErrSealingDirRequired == nil {
		t.Fatal("ErrSealingDirRequired is nil — sentinel must be exported")
	}
}

// TestStoreNewRequiresSealingDir — Options.SealingDir이 비면 ErrSealingDirRequired.
// (Linux/Other 모두 동일 동작 — SealingDir 검증이 TPM 디바이스 open보다 먼저 수행)
func TestStoreNewRequiresSealingDir(t *testing.T) {
	t.Parallel()
	_, err := tpm.New(tpm.Options{SealingDir: ""})
	if !errors.Is(err, tpm.ErrSealingDirRequired) {
		t.Errorf("New(SealingDir=\"\") err = %v, want ErrSealingDirRequired", err)
	}
}
