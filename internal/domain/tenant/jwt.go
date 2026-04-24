package tenant

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// JWT 결정 (B3·B4·B5):
//   - 알고리즘: EdDSA (Ed25519). `jwt.SigningMethodEdDSA`.
//   - 키 타입 비대칭: Sign은 `crypto.Signer`(`ed25519.PrivateKey`), Verify는 `ed25519.PublicKey` concrete.
//   - issuer/audience 단일 시스템.
//   - access 15분, refresh 14일 (Service Deps에서 override 가능).
const (
	JWTIssuer         = "rosshield"
	JWTAudience       = "rosshield-api"
	DefaultAccessTTL  = 15 * time.Minute
	DefaultRefreshTTL = 14 * 24 * time.Hour
	JWTLeeway         = 30 * time.Second // clock skew 허용 폭
)

// AccessClaims는 access 토큰의 디코딩 결과입니다.
type AccessClaims struct {
	Subject   string           // user ID (us_...)
	TenantID  storage.TenantID // tid claim
	Roles     []string         // role 이름 슬라이스
	ExpiresAt time.Time
	IssuedAt  time.Time
	JTI       string
}

// RefreshClaims는 refresh 토큰의 디코딩 결과입니다.
type RefreshClaims struct {
	Subject   string
	TenantID  storage.TenantID
	JTI       string
	ExpiresAt time.Time
	IssuedAt  time.Time
}

// internal — JWT 라이브러리에 넘기는 claim struct.
type accessJWT struct {
	TenantID string   `json:"tid"`
	Roles    []string `json:"roles"`
	jwt.RegisteredClaims
}

type refreshJWT struct {
	TenantID string `json:"tid"`
	jwt.RegisteredClaims
}

// SignAccessToken은 AccessClaims를 EdDSA로 서명한 JWT 문자열을 반환합니다.
func SignAccessToken(privKey ed25519.PrivateKey, c AccessClaims) (string, error) {
	if len(privKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("tenant: invalid Ed25519 private key size %d", len(privKey))
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, accessJWT{
		TenantID: string(c.TenantID),
		Roles:    c.Roles,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   c.Subject,
			Issuer:    JWTIssuer,
			Audience:  jwt.ClaimStrings{JWTAudience},
			IssuedAt:  jwt.NewNumericDate(c.IssuedAt),
			ExpiresAt: jwt.NewNumericDate(c.ExpiresAt),
			ID:        c.JTI,
		},
	})
	s, err := tok.SignedString(privKey)
	if err != nil {
		return "", fmt.Errorf("tenant: sign access token: %w", err)
	}
	return s, nil
}

// SignRefreshToken은 RefreshClaims를 EdDSA로 서명합니다.
func SignRefreshToken(privKey ed25519.PrivateKey, c RefreshClaims) (string, error) {
	if len(privKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("tenant: invalid Ed25519 private key size %d", len(privKey))
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, refreshJWT{
		TenantID: string(c.TenantID),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   c.Subject,
			Issuer:    JWTIssuer,
			Audience:  jwt.ClaimStrings{JWTAudience},
			IssuedAt:  jwt.NewNumericDate(c.IssuedAt),
			ExpiresAt: jwt.NewNumericDate(c.ExpiresAt),
			ID:        c.JTI,
		},
	})
	s, err := tok.SignedString(privKey)
	if err != nil {
		return "", fmt.Errorf("tenant: sign refresh token: %w", err)
	}
	return s, nil
}

// ParseAccessToken은 token 문자열을 검증·디코딩하여 AccessClaims를 반환합니다.
//
// 실패 분기:
//   - 알고리즘 불일치: ErrInvalidToken (alg=none 방지)
//   - 만료: ErrTokenExpired
//   - 서명 불일치: ErrTokenSignatureInvalid
//   - 그 외 형식 오류: ErrInvalidToken
func ParseAccessToken(pubKey ed25519.PublicKey, token string) (AccessClaims, error) {
	var c accessJWT
	parsed, err := jwt.ParseWithClaims(token, &c, keyfunc(pubKey),
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithIssuer(JWTIssuer),
		jwt.WithAudience(JWTAudience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(JWTLeeway),
	)
	if err != nil {
		return AccessClaims{}, mapJWTError(err)
	}
	if !parsed.Valid {
		return AccessClaims{}, ErrInvalidToken
	}
	exp := time.Time{}
	iat := time.Time{}
	if c.ExpiresAt != nil {
		exp = c.ExpiresAt.Time
	}
	if c.IssuedAt != nil {
		iat = c.IssuedAt.Time
	}
	return AccessClaims{
		Subject:   c.Subject,
		TenantID:  storage.TenantID(c.TenantID),
		Roles:     c.Roles,
		ExpiresAt: exp,
		IssuedAt:  iat,
		JTI:       c.ID,
	}, nil
}

// ParseRefreshToken은 refresh 토큰을 검증·디코딩합니다.
func ParseRefreshToken(pubKey ed25519.PublicKey, token string) (RefreshClaims, error) {
	var c refreshJWT
	parsed, err := jwt.ParseWithClaims(token, &c, keyfunc(pubKey),
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithIssuer(JWTIssuer),
		jwt.WithAudience(JWTAudience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(JWTLeeway),
	)
	if err != nil {
		return RefreshClaims{}, mapJWTError(err)
	}
	if !parsed.Valid {
		return RefreshClaims{}, ErrInvalidToken
	}
	exp := time.Time{}
	iat := time.Time{}
	if c.ExpiresAt != nil {
		exp = c.ExpiresAt.Time
	}
	if c.IssuedAt != nil {
		iat = c.IssuedAt.Time
	}
	return RefreshClaims{
		Subject:   c.Subject,
		TenantID:  storage.TenantID(c.TenantID),
		JTI:       c.ID,
		ExpiresAt: exp,
		IssuedAt:  iat,
	}, nil
}

func keyfunc(pubKey ed25519.PublicKey) jwt.Keyfunc {
	return func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, ErrInvalidToken
		}
		return pubKey, nil
	}
}

// mapJWTError는 jwt v5 에러를 도메인 sentinel로 매핑합니다.
func mapJWTError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return ErrTokenExpired
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return ErrTokenSignatureInvalid
	case errors.Is(err, jwt.ErrTokenMalformed),
		errors.Is(err, jwt.ErrTokenNotValidYet),
		errors.Is(err, jwt.ErrTokenInvalidAudience),
		errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return ErrInvalidToken
	default:
		// 알 수 없는 에러는 ErrInvalidToken으로 안전하게 매핑
		return fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
}

// JWT 관련 sentinel 에러.
var (
	ErrInvalidToken          = errors.New("tenant: invalid token")
	ErrTokenExpired          = errors.New("tenant: token expired")
	ErrTokenSignatureInvalid = errors.New("tenant: token signature invalid")
	ErrInvalidCredentials    = errors.New("tenant: invalid credentials")
	ErrUserDisabled          = errors.New("tenant: user is disabled")
	ErrRefreshNotFound       = errors.New("tenant: refresh token not found")
	ErrRefreshRevoked        = errors.New("tenant: refresh token has been revoked")
	ErrRefreshExpired        = errors.New("tenant: refresh token has expired")
)
