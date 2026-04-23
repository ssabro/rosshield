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
	"fmt"

	"github.com/ssabro/rosshield/internal/platform/signer"
)

type softSigner struct {
	private ed25519.PrivateKey
	public  ed25519.PublicKey
	keyID   string
}

// New는 새 Ed25519 키 쌍을 생성하여 Signer를 반환합니다.
// 매 호출마다 새 키 — 영속이 필요하면 후속 에픽의 LoadFromFile 등 사용.
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
