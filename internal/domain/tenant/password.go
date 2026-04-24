package tenant

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id 파라미터 (B2 결정 — 백로그 §E3.T2 명시값).
//
// memory=64MiB, time=3, parallelism=1 — OWASP 2023 권장 minimum 이상.
// keyLen=32B (256-bit), saltLen=16B.
const (
	argonMemory     uint32 = 64 * 1024 // KiB → 64 MiB
	argonTime       uint32 = 3
	argonThreads    uint8  = 1
	argonKeyLen     uint32 = 32
	argonSaltLen           = 16
	argonVersionStr        = "v=19" // argon2.Version (19) hex 표기
)

// HashPassword는 raw 비밀번호를 argon2id 형식으로 인코딩합니다.
//
// 출력 형식 (PHC string format):
//
//	$argon2id$v=19$m=65536,t=3,p=1$<base64-salt>$<base64-hash>
//
// salt는 매 호출마다 새로 생성됩니다 (16B crypto/rand).
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", ErrEmptyPassword
	}

	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("tenant: read salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	encoded := fmt.Sprintf("$argon2id$%s$m=%d,t=%d,p=%d$%s$%s",
		argonVersionStr,
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash))
	return encoded, nil
}

// VerifyPassword는 raw 비밀번호와 인코딩된 해시를 비교합니다.
//
// 일치하면 nil, 불일치면 ErrInvalidPasswordCheck.
// 형식이 잘못된 경우 ErrPasswordHashMalformed.
func VerifyPassword(password, encoded string) error {
	if password == "" || encoded == "" {
		return ErrInvalidPasswordCheck
	}

	memory, time, threads, salt, want, err := decodeArgonHash(encoded)
	if err != nil {
		return err
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrInvalidPasswordCheck
	}
	return nil
}

func decodeArgonHash(encoded string) (memory, time uint32, threads uint8, salt, hash []byte, err error) {
	parts := strings.Split(encoded, "$")
	// 빈 prefix($) 포함 6 parts: ["", "argon2id", "v=19", "m=...,t=...,p=...", "<salt>", "<hash>"]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, ErrPasswordHashMalformed
	}
	if parts[2] != argonVersionStr {
		return 0, 0, 0, nil, nil, ErrPasswordHashMalformed
	}
	if _, scanErr := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); scanErr != nil {
		return 0, 0, 0, nil, nil, ErrPasswordHashMalformed
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return 0, 0, 0, nil, nil, ErrPasswordHashMalformed
	}
	hash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return 0, 0, 0, nil, nil, ErrPasswordHashMalformed
	}
	return memory, time, threads, salt, hash, nil
}
