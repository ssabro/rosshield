// Package soft는 메모리 보관 Ed25519 키 기반 Signer입니다.
//
// 프로세스 재시작 시 키가 사라집니다 (의도). 영속 키는 후속 에픽에서:
//   - E2 audit checkpoint: 디스크에 보관된 키를 부팅 시 로드
//   - Phase 3 SKU: HSM/TPM 어댑터
//
// Ed25519 선택 근거 (§11.3): stdlib `crypto/ed25519`로 외부 의존 0,
// 결정론적·고속·작은 시그니처(64 bytes).
package soft

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ssabro/rosshield/internal/platform/signer"
)

type softSigner struct {
	private ed25519.PrivateKey
	public  ed25519.PublicKey
	keyID   string
}

// New는 새 Ed25519 키 쌍을 생성하여 Signer를 반환합니다.
// 매 호출마다 새 키 — 영속이 필요하면 LoadOrCreate 사용.
func New() (signer.Signer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("soft: ed25519.GenerateKey: %w", err)
	}
	return &softSigner{
		private: priv,
		public:  pub,
		keyID:   makeKeyID(pub),
	}, nil
}

// LoadOrCreate은 path에서 Ed25519 private key를 로드하거나 없으면 새로 생성하여 저장합니다.
//
// 파일 형식: ed25519 private key raw bytes (64B = seed 32B + public 32B).
// 디렉토리는 0700, 파일은 0600 권한으로 생성. 부모 디렉토리는 자동 생성.
//
// 두 번째 호출부터는 같은 keyID를 반환합니다 (프로세스 재시작 후 checkpoint 검증을 위해 필수).
func LoadOrCreate(path string) (signer.Signer, error) {
	priv, err := LoadOrCreatePrivateKey(path)
	if err != nil {
		return nil, err
	}
	return wrapPrivateKey(priv), nil
}

// LoadOrCreatePrivateKey는 LoadOrCreate와 동일하지만 raw `ed25519.PrivateKey`를 직접 반환합니다.
//
// JWT·기타 표준 라이브러리가 raw key를 요구하는 경우 사용 (`golang-jwt/jwt/v5` 등).
// signer 인터페이스 우회 — 일반 audit checkpoint 등에는 LoadOrCreate 사용.
func LoadOrCreatePrivateKey(path string) (ed25519.PrivateKey, error) {
	if path == "" {
		return nil, fmt.Errorf("soft: LoadOrCreatePrivateKey requires non-empty path")
	}

	if priv, err := loadKeyFromFile(path); err == nil {
		return priv, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("soft: mkdir keys dir %q: %w", filepath.Dir(path), err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("soft: ed25519.GenerateKey: %w", err)
	}
	if err := writeKeyToFile(path, priv); err != nil {
		return nil, err
	}
	return priv, nil
}

func loadKeyFromFile(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("soft: key file %q has size %d, want %d", path, len(data), ed25519.PrivateKeySize)
	}
	priv := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	copy(priv, data)
	return priv, nil
}

func writeKeyToFile(path string, priv ed25519.PrivateKey) error {
	if err := os.WriteFile(path, priv, 0o600); err != nil {
		return fmt.Errorf("soft: write key %q: %w", path, err)
	}
	return nil
}

func wrapPrivateKey(priv ed25519.PrivateKey) signer.Signer {
	return WrapPrivateKey(priv)
}

// WrapPrivateKey는 외부에서 얻은 ed25519 private key(예: TPM이 unseal한 raw key)를
// soft signer로 감쌉니다 (E34 keystore 어댑터 결선용).
//
// 본 함수는 키 영속을 수행하지 않습니다 — caller가 keystore 측에서 영속 책임.
func WrapPrivateKey(priv ed25519.PrivateKey) signer.Signer {
	pub := priv.Public().(ed25519.PublicKey)
	return &softSigner{private: priv, public: pub, keyID: makeKeyID(pub)}
}

// makeKeyID는 공개키 sha256의 첫 8 bytes를 hex로 표현하여 안정적 키 식별자를 만듭니다.
// 형식: "key_<16-hex>" (예: "key_a3f1c9b27e840a2c").
func makeKeyID(pub ed25519.PublicKey) string {
	digest := sha256.Sum256(pub)
	return "key_" + hex.EncodeToString(digest[:8])
}

func (s *softSigner) Sign(payload []byte) ([]byte, string, error) {
	sig := ed25519.Sign(s.private, payload)
	return sig, s.keyID, nil
}

func (s *softSigner) Verify(payload, sig []byte) error {
	if len(sig) != ed25519.SignatureSize {
		return signer.ErrShortSignature
	}
	if !ed25519.Verify(s.public, payload, sig) {
		return signer.ErrInvalidSignature
	}
	return nil
}

func (s *softSigner) PublicKey() []byte {
	out := make([]byte, len(s.public))
	copy(out, s.public)
	return out
}

func (s *softSigner) KeyID() string {
	return s.keyID
}
