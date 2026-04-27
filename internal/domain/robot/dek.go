package robot

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// DEKSizeBytes는 per-record DEK(Data Encryption Key) 길이입니다.
const DEKSizeBytes = 32

// gcmNonceSize는 AES-GCM nonce 길이입니다 (12B 표준).
const gcmNonceSize = 12

// WrapMaterial은 평문 CredentialMaterial을 암호화합니다.
//
// 절차 (envelope encryption — KEK→DEK 2계층, R3-2):
//  1. random DEK 생성 (32B, AES-256).
//  2. random DEKNonce 12B 생성, KEK로 DEK를 wrap (AES-256-GCM, AAD = `ctx`).
//  3. random PayloadNonce 12B 생성, DEK로 material JSON 암호화.
//  4. EncryptionMeta 조립.
//
// AAD는 `t=<tenantID>;c=<credentialID>;v=1` — credential·tenant 짝이 변경되면 unwrap 실패 → cross-credential 키 재사용 차단.
func WrapMaterial(kek *KEK, tenantID storage.TenantID, credentialID string, material CredentialMaterial, now time.Time) ([]byte, EncryptionMeta, error) {
	if kek == nil {
		return nil, EncryptionMeta{}, errors.New("robot: KEK is required")
	}
	if tenantID == "" {
		return nil, EncryptionMeta{}, storage.ErrTenantMissing
	}
	if credentialID == "" {
		return nil, EncryptionMeta{}, errors.New("robot: credentialID is required")
	}
	if err := validateMaterial(material); err != nil {
		return nil, EncryptionMeta{}, err
	}

	plaintext, err := json.Marshal(material)
	if err != nil {
		return nil, EncryptionMeta{}, fmt.Errorf("robot: marshal material: %w", err)
	}

	dek := make([]byte, DEKSizeBytes)
	if _, err := rand.Read(dek); err != nil {
		return nil, EncryptionMeta{}, fmt.Errorf("robot: DEK rand: %w", err)
	}

	dekNonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(dekNonce); err != nil {
		return nil, EncryptionMeta{}, fmt.Errorf("robot: DEKNonce rand: %w", err)
	}
	payloadNonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(payloadNonce); err != nil {
		return nil, EncryptionMeta{}, fmt.Errorf("robot: PayloadNonce rand: %w", err)
	}

	aad := buildAAD(tenantID, credentialID)

	// DEK를 KEK로 wrap (AAD 포함).
	wrappedDEK, err := gcmSeal(kek.rawBytes(), dekNonce, dek, []byte(aad))
	if err != nil {
		return nil, EncryptionMeta{}, fmt.Errorf("robot: wrap DEK: %w", err)
	}

	// payload를 DEK로 encrypt (같은 AAD).
	ciphertext, err := gcmSeal(dek, payloadNonce, plaintext, []byte(aad))
	if err != nil {
		return nil, EncryptionMeta{}, fmt.Errorf("robot: encrypt payload: %w", err)
	}

	meta := EncryptionMeta{
		Version:      EncryptionVersion,
		Algorithm:    EncryptionAlgorithm,
		KEKKeyID:     kek.KeyID(),
		AAD:          aad,
		DEKNonce:     dekNonce,
		PayloadNonce: payloadNonce,
		WrappedDEK:   wrappedDEK,
		CreatedAt:    now.UTC(),
	}
	return ciphertext, meta, nil
}

// UnwrapMaterial은 암호화된 payload를 평문 CredentialMaterial로 복호화합니다.
//
// 함정: KEK가 다르거나 ciphertext·meta가 변조됐으면 ErrCredentialDecrypt.
// AAD에 tenantID·credentialID가 포함되므로 다른 credential의 ciphertext를 빌려와 위조 시도 시도 시 실패.
func UnwrapMaterial(kek *KEK, encryptedPayload []byte, meta EncryptionMeta) (CredentialMaterial, error) {
	if kek == nil {
		return CredentialMaterial{}, errors.New("robot: KEK is required")
	}
	if meta.Version != EncryptionVersion {
		return CredentialMaterial{}, fmt.Errorf("%w: got %d, want %d", ErrCredentialMetaVersion, meta.Version, EncryptionVersion)
	}
	if meta.Algorithm != EncryptionAlgorithm {
		return CredentialMaterial{}, fmt.Errorf("robot: unsupported algorithm %q", meta.Algorithm)
	}
	if meta.KEKKeyID != kek.KeyID() {
		// 키 회전 후 다른 KEK로 wrap된 레코드 — 회전 도구가 별도. Phase 1 미구현.
		return CredentialMaterial{}, fmt.Errorf("%w: meta KEKKeyID=%q, current KEK=%q", ErrCredentialDecrypt, meta.KEKKeyID, kek.KeyID())
	}

	dek, err := gcmOpen(kek.rawBytes(), meta.DEKNonce, meta.WrappedDEK, []byte(meta.AAD))
	if err != nil {
		return CredentialMaterial{}, fmt.Errorf("%w: wrap DEK: %v", ErrCredentialDecrypt, err)
	}
	defer zero(dek) // 사용 후 메모리 폐기.

	plaintext, err := gcmOpen(dek, meta.PayloadNonce, encryptedPayload, []byte(meta.AAD))
	if err != nil {
		return CredentialMaterial{}, fmt.Errorf("%w: payload: %v", ErrCredentialDecrypt, err)
	}

	var material CredentialMaterial
	if err := json.Unmarshal(plaintext, &material); err != nil {
		return CredentialMaterial{}, fmt.Errorf("%w: unmarshal: %v", ErrCredentialDecrypt, err)
	}
	return material, nil
}

// gcmSeal은 AES-256-GCM으로 plaintext를 암호화합니다 (key 32B 강제).
func gcmSeal(key, nonce, plaintext, aad []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("robot: AES key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("robot: NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("robot: NewGCM: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("robot: nonce must be %d bytes, got %d", gcm.NonceSize(), len(nonce))
	}
	return gcm.Seal(nil, nonce, plaintext, aad), nil
}

// gcmOpen은 AES-256-GCM ciphertext를 복호화합니다 (key 32B 강제).
func gcmOpen(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("robot: AES key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("robot: nonce must be %d bytes, got %d", gcm.NonceSize(), len(nonce))
	}
	return gcm.Open(nil, nonce, ciphertext, aad)
}

func validateMaterial(m CredentialMaterial) error {
	switch m.Type {
	case CredentialTypePassword, CredentialTypePrivateKey:
		// OK
	default:
		return ErrCredentialUnknownType
	}
	if strings.TrimSpace(m.Username) == "" {
		return ErrCredentialEmptyUser
	}
	return nil
}

func buildAAD(tenantID storage.TenantID, credentialID string) string {
	return "t=" + string(tenantID) + ";c=" + credentialID + ";v=" + intStr(EncryptionVersion)
}

func intStr(n int) string {
	// 작은 양수 전용 — strconv 의존 회피.
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// zero는 byte slice를 0으로 덮어씁니다 (메모리 잔존 최소화).
// Go는 GC 언어라 완전한 보장은 어렵지만 noisefloor 감소.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
