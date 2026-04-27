package robot

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// KEKSizeBytes는 KEK(Key Encryption Key) 길이입니다. AES-256 키 크기.
const KEKSizeBytes = 32

// KEK는 Phase 1의 마스터 키입니다 (R3-1 — 파일 기반 `<dataDir>/keys/credential.kek`, perm 0600).
//
// Phase 2+에서 OS Keychain·KMS·TPM 봉인으로 전환 가능 — 표면(KeyID·wrap·unwrap)을 안정시켜
// 어댑터 교체로 마이그레이션. KEK 본체(`raw`)는 메모리 외 노출 금지.
type KEK struct {
	raw   []byte // 32B
	keyID string
}

// LoadOrCreateKEK는 path의 KEK를 로드합니다. 부재 시 32B 랜덤 KEK를 생성·저장합니다.
//
// 파일 권한:
//   - 디렉터리: 0700 (없으면 생성).
//   - 파일: 0600 (Unix). Windows는 ACL 별도 — 후순위 (Windows ACL 키 파일 보호).
//
// 동일 path로 여러 번 호출 시 항상 같은 KeyID 반환 (Signer.LoadOrCreate 패턴 동일).
func LoadOrCreateKEK(path string) (*KEK, error) {
	if path == "" {
		return nil, errors.New("robot: KEK path is required")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("robot: KEK mkdir %q: %w", dir, err)
	}

	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		// 기존 키 검증.
		if len(raw) != KEKSizeBytes {
			return nil, fmt.Errorf("%w: got %d bytes at %q", ErrKEKInvalidLength, len(raw), path)
		}
		if err := verifyKeyFilePerm(path); err != nil {
			return nil, err
		}
		return newKEK(raw), nil

	case errors.Is(err, fs.ErrNotExist):
		// 신규 키 생성.
		raw = make([]byte, KEKSizeBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, fmt.Errorf("robot: KEK rand.Read: %w", err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return nil, fmt.Errorf("robot: KEK write %q: %w", path, err)
		}
		return newKEK(raw), nil

	default:
		return nil, fmt.Errorf("robot: KEK read %q: %w", path, err)
	}
}

// KeyID는 KEK 식별자를 반환합니다 ("kek_<sha256(raw)[:8] hex>" — 총 20자).
//
// EncryptionMeta.KEKKeyID에 저장돼 회전·검증·키 분실 진단에 사용.
func (k *KEK) KeyID() string {
	return k.keyID
}

// rawBytes는 내부 wrap/unwrap 함수에만 노출합니다 (같은 패키지).
func (k *KEK) rawBytes() []byte {
	return k.raw
}

func newKEK(raw []byte) *KEK {
	sum := sha256.Sum256(raw)
	return &KEK{
		raw:   raw,
		keyID: "kek_" + hex.EncodeToString(sum[:4]), // 8 hex chars
	}
}

// verifyKeyFilePerm은 Unix에서 파일 권한이 0600 이하인지 검증합니다.
// Windows는 권한 모델이 다르므로 skip — Phase 2+에 ACL 검증 추가 예정 (핸드오프 후순위 9번).
func verifyKeyFilePerm(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("robot: KEK stat %q: %w", path, err)
	}
	mode := info.Mode().Perm()
	// group·other에 read/write/execute 권한이 있으면 거부 (0o077 마스크).
	if mode&0o077 != 0 {
		return fmt.Errorf("%w: %q has perm %o", ErrKEKFilePermissions, path, mode)
	}
	return nil
}
