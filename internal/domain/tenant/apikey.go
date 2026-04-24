package tenant

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
)

// ApiKey 토큰 형식 (§5.9):
//
//	fg_live_<32 chars Crockford base32>
//
// 총 길이: prefixLen(8 "fg_live_") + bodyLen(32) = 40자.
// `Prefix` (DB 저장·표시용)는 토큰 앞 12자 — "fg_live_" + 4자.
const (
	apiKeyTokenPrefix = "fg_live_"
	apiKeyBodyLen     = 32 // base32 chars
	apiKeyDisplayLen  = 12 // prefix 길이 ("fg_live_" + 4 random chars)
)

// crockford base32 (no padding) — ULID와 같은 알파벳, 사람 친화적.
// 표준 base32(RFC 4648)는 0/1/I/L 같은 모호 문자 포함이지만 Crockford는 그걸 피함.
// jwt 라이브러리·외부 도구가 base32 표준을 가정해도 문제 없도록 RFC 4648 사용 (단순화).
var apiKeyBodyEnc = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateApiKeyToken은 새로운 raw token을 생성합니다.
//
// 반환값: token(전체 40자), prefix(앞 12자, DB 저장용).
// raw token은 발급 시점에 호출자에게 한 번만 노출됩니다.
func GenerateApiKeyToken() (token, prefix string, err error) {
	// base32 32자 = 20바이트 (8/5 비율, 32*5/8=20).
	const rawBytes = 20
	buf := make([]byte, rawBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("tenant: read random: %w", err)
	}
	body := apiKeyBodyEnc.EncodeToString(buf)
	if len(body) != apiKeyBodyLen {
		return "", "", fmt.Errorf("tenant: unexpected body len %d, want %d", len(body), apiKeyBodyLen)
	}
	token = apiKeyTokenPrefix + body
	prefix = token[:apiKeyDisplayLen]
	return token, prefix, nil
}

// ExtractApiKeyPrefix는 raw token에서 prefix(앞 12자)를 추출합니다.
//
// 형식이 잘못된 경우 ErrInvalidApiKeyFormat — DB lookup 전 빠른 차단.
func ExtractApiKeyPrefix(token string) (string, error) {
	if !strings.HasPrefix(token, apiKeyTokenPrefix) {
		return "", ErrInvalidApiKeyFormat
	}
	if len(token) != apiKeyDisplayLen-4+apiKeyBodyLen {
		// 8 + 32 = 40
		return "", ErrInvalidApiKeyFormat
	}
	return token[:apiKeyDisplayLen], nil
}

// API key 관련 에러.
var (
	ErrInvalidApiKeyFormat = errors.New("tenant: api key format invalid")
	ErrApiKeyNotFound      = errors.New("tenant: api key not found")
	ErrApiKeyRevoked       = errors.New("tenant: api key has been revoked")
	ErrApiKeyExpired       = errors.New("tenant: api key has expired")
)
