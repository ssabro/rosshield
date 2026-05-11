package tpm_test

import (
	"errors"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/keystore/tpm"
)

// TestTpmPlaceholderReturnsNotImplemented — Stage 1 placeholder는 모든 호출에서
// ErrTpmNotImplemented 반환. Stage 2 이후 본격 구현 시 본 테스트는 갱신됨.
func TestTpmPlaceholderReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	s := tpm.New()
	_, err := s.LoadOrCreatePrivateKey("any-handle")
	if !errors.Is(err, tpm.ErrTpmNotImplemented) {
		t.Errorf("LoadOrCreatePrivateKey err = %v, want ErrTpmNotImplemented", err)
	}
}

// TestTpmCloseIsNoop — Stage 1 placeholder Close는 nil.
func TestTpmCloseIsNoop(t *testing.T) {
	t.Parallel()
	s := tpm.New()
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v, want nil", err)
	}
}
