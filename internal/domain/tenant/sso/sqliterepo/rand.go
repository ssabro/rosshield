package sqliterepo

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// randomToken은 nBytes 만큼 crypto/rand로 채운 base64url(no padding) 토큰을 반환합니다.
//
// 사용처:
//   - state (CSRF token) 32바이트 → 43자.
//   - PKCE code_verifier 64바이트 → 86자 (RFC 7636: 43~128 문자 허용).
//   - nonce 32바이트.
//
// crypto/rand 실패는 시스템 한계 — 호출자가 즉시 fail-fast.
func randomToken(nBytes int) (string, error) {
	if nBytes <= 0 {
		return "", fmt.Errorf("sso: token size must be > 0, got %d", nBytes)
	}
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("sso: crypto/rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
