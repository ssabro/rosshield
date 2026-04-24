package soft_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
)

func newSigner(t *testing.T) signer.Signer {
	t.Helper()
	s, err := soft.New()
	if err != nil {
		t.Fatalf("soft.New: %v", err)
	}
	return s
}

func TestSignerEd25519SignVerifyRoundtrip(t *testing.T) {
	t.Parallel()

	s := newSigner(t)
	payload := []byte("hello rosshield audit chain")

	sig, keyID, err := s.Sign(payload)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) != ed25519.SignatureSize {
		t.Errorf("sig length = %d, want %d", len(sig), ed25519.SignatureSize)
	}
	if keyID != s.KeyID() {
		t.Errorf("Sign returned keyID %q, want %q (matches Signer.KeyID())", keyID, s.KeyID())
	}

	if err := s.Verify(payload, sig); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestSignerRejectsTamperedPayload(t *testing.T) {
	t.Parallel()

	s := newSigner(t)
	payload := []byte("original payload")

	sig, _, err := s.Sign(payload)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	tampered := []byte("original paylo@d") // 1 byte 변경
	err = s.Verify(tampered, sig)
	if !errors.Is(err, signer.ErrInvalidSignature) {
		t.Errorf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestSignerRejectsTamperedSignature(t *testing.T) {
	t.Parallel()

	s := newSigner(t)
	payload := []byte("payload")

	sig, _, err := s.Sign(payload)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// 시그니처 마지막 바이트 변경.
	tampered := make([]byte, len(sig))
	copy(tampered, sig)
	tampered[len(tampered)-1] ^= 0x01

	err = s.Verify(payload, tampered)
	if !errors.Is(err, signer.ErrInvalidSignature) {
		t.Errorf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestSignerRejectsShortSignature(t *testing.T) {
	t.Parallel()

	s := newSigner(t)

	err := s.Verify([]byte("payload"), []byte("too-short"))
	if !errors.Is(err, signer.ErrShortSignature) {
		t.Errorf("err = %v, want ErrShortSignature", err)
	}
}

func TestSignerKeyIDIsStableAndFormatted(t *testing.T) {
	t.Parallel()

	s := newSigner(t)
	id1 := s.KeyID()
	id2 := s.KeyID()

	if id1 != id2 {
		t.Errorf("KeyID not stable across calls: %q vs %q", id1, id2)
	}
	if !strings.HasPrefix(id1, "key_") {
		t.Errorf("KeyID = %q, want key_ prefix", id1)
	}
	// "key_" + 16 hex chars = 20.
	if len(id1) != 20 {
		t.Errorf("KeyID length = %d, want 20", len(id1))
	}

	// KeyID는 sha256(publicKey)[:8] hex와 일치해야 함.
	pub := s.PublicKey()
	digest := sha256.Sum256(pub)
	want := "key_" + hex.EncodeToString(digest[:8])
	if id1 != want {
		t.Errorf("KeyID = %q, want %q (sha256(publicKey)[:8] hex)", id1, want)
	}
}

func TestSignerPublicKeyMatchesSignaturePath(t *testing.T) {
	t.Parallel()

	s := newSigner(t)
	pub := s.PublicKey()

	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("public key length = %d, want %d", len(pub), ed25519.PublicKeySize)
	}

	// Signer가 반환한 공개키로 외부 ed25519.Verify가 동일 결과를 내야 함.
	payload := []byte("verify with returned public key")
	sig, _, err := s.Sign(payload)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), payload, sig) {
		t.Error("ed25519.Verify with PublicKey() failed — signer 내부와 외부 검증이 불일치")
	}
}

func TestLoadOrCreateGeneratesAndPersists(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "platform.ed25519")

	s1, err := soft.LoadOrCreate(path)
	if err != nil {
		t.Fatalf("first LoadOrCreate: %v", err)
	}
	if s1.KeyID() == "" {
		t.Error("KeyID empty after generate")
	}

	// 파일이 실제로 생성되었어야 함.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat key file: %v", err)
	}
	if info.Size() != int64(ed25519.PrivateKeySize) {
		t.Errorf("key file size = %d, want %d", info.Size(), ed25519.PrivateKeySize)
	}

	// 두 번째 호출은 같은 키.
	s2, err := soft.LoadOrCreate(path)
	if err != nil {
		t.Fatalf("second LoadOrCreate: %v", err)
	}
	if s1.KeyID() != s2.KeyID() {
		t.Errorf("KeyID changed across reload: %q → %q", s1.KeyID(), s2.KeyID())
	}

	// s1의 서명을 s2가 검증해야 함 (같은 키).
	payload := []byte("persisted key roundtrip")
	sig, _, err := s1.Sign(payload)
	if err != nil {
		t.Fatalf("s1.Sign: %v", err)
	}
	if err := s2.Verify(payload, sig); err != nil {
		t.Errorf("s2.Verify(s1's sig) failed: %v — keys must be identical", err)
	}
}

func TestLoadOrCreateAutoCreatesParentDir(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "deep", "nested", "key.ed25519")
	s, err := soft.LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if s.KeyID() == "" {
		t.Error("KeyID empty")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("key file not created: %v", err)
	}
}

func TestLoadOrCreateRejectsCorruptFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "corrupt.ed25519")
	if err := os.WriteFile(path, []byte("not a valid ed25519 key"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := soft.LoadOrCreate(path)
	if err == nil {
		t.Fatal("expected error for corrupt key file")
	}
}

func TestLoadOrCreateRequiresPath(t *testing.T) {
	t.Parallel()

	_, err := soft.LoadOrCreate("")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestSignerInstancesGenerateDistinctKeys(t *testing.T) {
	t.Parallel()

	a := newSigner(t)
	b := newSigner(t)

	if a.KeyID() == b.KeyID() {
		t.Errorf("two independent signers produced same KeyID: %q", a.KeyID())
	}

	// a로 서명한 것을 b로 검증하면 실패해야 함 (다른 키).
	payload := []byte("crossed wires")
	sig, _, err := a.Sign(payload)
	if err != nil {
		t.Fatalf("a.Sign: %v", err)
	}
	if err := b.Verify(payload, sig); !errors.Is(err, signer.ErrInvalidSignature) {
		t.Errorf("b.Verify(a's sig) err = %v, want ErrInvalidSignature", err)
	}
}
