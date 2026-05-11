package file_test

import (
	"crypto/ed25519"
	"path/filepath"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/keystore/file"
)

// TestFileStoreLoadOrCreateRoundTrip — 첫 호출은 키 생성 + 저장, 두 번째는 같은 키 로드.
func TestFileStoreLoadOrCreateRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "platform.ed25519")

	s := file.New()
	defer func() { _ = s.Close() }()

	priv1, err := s.LoadOrCreatePrivateKey(keyPath)
	if err != nil {
		t.Fatalf("first LoadOrCreate: %v", err)
	}
	if len(priv1) != ed25519.PrivateKeySize {
		t.Errorf("priv1 size = %d, want %d", len(priv1), ed25519.PrivateKeySize)
	}

	// 두 번째 호출 — 같은 키 로드.
	priv2, err := s.LoadOrCreatePrivateKey(keyPath)
	if err != nil {
		t.Fatalf("second LoadOrCreate: %v", err)
	}
	if string(priv1) != string(priv2) {
		t.Errorf("priv2 differs from priv1 (LoadOrCreate not deterministic)")
	}

	// 새 Store 인스턴스에서도 같은 키 로드 (process restart 시뮬).
	s2 := file.New()
	priv3, err := s2.LoadOrCreatePrivateKey(keyPath)
	if err != nil {
		t.Fatalf("new Store LoadOrCreate: %v", err)
	}
	if string(priv1) != string(priv3) {
		t.Errorf("priv3 differs from priv1 across Store instances")
	}
}

// TestFileStoreCloseIsNoop — Close는 no-op이어야 함 (file 어댑터는 영속 리소스 없음).
func TestFileStoreCloseIsNoop(t *testing.T) {
	t.Parallel()
	s := file.New()
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v, want nil", err)
	}
	// 두 번 호출도 안전.
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v, want nil", err)
	}
}
