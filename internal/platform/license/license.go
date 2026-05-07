// Package license는 rosshield Enterprise SKU의 라이선스 토큰 발급·검증·쿼터 게이트입니다 (E24).
//
// 설계:
//   - 라이선스 토큰은 Ed25519 서명된 JSON payload (오프라인 검증 — P3 에어갭).
//   - 빌드 시 `LicensePublicKey`를 임베드해 외부 통신 없이 검증 가능.
//   - Open-core 게이트: SSO·MT 관리·Webhook·Cloud dashboard 같은 enterprise endpoint
//     진입 시 Verifier로 라이선스 + tenant 한도(quota) 검사.
//   - 결정론적·외부 검증 가능 (P1) — 같은 토큰 + 같은 입력은 항상 같은 결과.
//
// 토큰 형식:
//
//	{
//	  "v": 1,
//	  "license_id": "lic_<ULID>",
//	  "issued_to": "Acme Corp",
//	  "issued_at": "2026-01-01T00:00:00Z",
//	  "expires_at": "2027-01-01T00:00:00Z",
//	  "edition": "enterprise",
//	  "features": ["sso","mt","webhook"],
//	  "quotas": {
//	    "robots_max": 100,
//	    "scans_per_day": 1000,
//	    "llm_tokens_per_day": 1000000
//	  }
//	}
//	+ "."  + base64url(ed25519 signature of canonical JSON)
//
// 즉 `<base64url(payload-json)>.<base64url(signature)>` 형식. JWT 유사 (alg 헤더 생략 — Ed25519 고정).
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Edition은 SKU 종류입니다.
type Edition string

const (
	EditionCommunity  Edition = "community"  // 라이선스 미부여 — 코어 기능만.
	EditionEnterprise Edition = "enterprise" // 모든 enterprise feature 활성.
)

// Feature는 게이트 가능한 enterprise 기능 식별자입니다.
type Feature string

const (
	FeatureSSO     Feature = "sso"     // OIDC + SAML
	FeatureMT      Feature = "mt"      // Multi-tenant 관리 + 격리 강화
	FeatureWebhook Feature = "webhook" // Webhook + SIEM 송신
	FeatureCloud   Feature = "cloud"   // Cloud dashboard·collaborative
	FeatureHA      Feature = "ha"      // Leader/follower 고가용성
)

// Quota는 라이선스에 명시된 한도입니다. 0 또는 음수는 "무제한"으로 해석.
type Quota struct {
	RobotsMax       int `json:"robots_max,omitempty"`
	ScansPerDay     int `json:"scans_per_day,omitempty"`
	LLMTokensPerDay int `json:"llm_tokens_per_day,omitempty"`
}

// IsUnlimited는 한도가 무제한(0 또는 음수)인지 판단합니다.
func (q Quota) IsUnlimited(field string) bool {
	switch field {
	case "robots_max":
		return q.RobotsMax <= 0
	case "scans_per_day":
		return q.ScansPerDay <= 0
	case "llm_tokens_per_day":
		return q.LLMTokensPerDay <= 0
	}
	return false
}

// Payload는 라이선스 토큰의 본문입니다 (서명 대상).
type Payload struct {
	Version   int       `json:"v"`
	LicenseID string    `json:"license_id"`
	IssuedTo  string    `json:"issued_to"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Edition   Edition   `json:"edition"`
	Features  []Feature `json:"features,omitempty"`
	Quotas    Quota     `json:"quotas,omitempty"`
}

// HasFeature는 본 라이선스가 특정 enterprise 기능을 활성화했는지 검사합니다.
func (p Payload) HasFeature(f Feature) bool {
	for _, x := range p.Features {
		if x == f {
			return true
		}
	}
	return false
}

// IsExpired는 now가 ExpiresAt을 지났는지 검사합니다 (현재 시각 주입 — 결정론).
func (p Payload) IsExpired(now time.Time) bool {
	return now.After(p.ExpiresAt)
}

// Sentinel errors.
var (
	ErrInvalidFormat    = errors.New("license: token format invalid (expected payload.signature)")
	ErrInvalidPayload   = errors.New("license: payload JSON decode failed")
	ErrInvalidSignature = errors.New("license: signature verification failed")
	ErrTokenExpired     = errors.New("license: token expired")
	ErrUnsupportedVer   = errors.New("license: unsupported version")
	ErrFeatureGated     = errors.New("license: feature requires enterprise license")
	ErrQuotaExceeded    = errors.New("license: quota exceeded")
)

// SupportedVersion은 본 모듈이 발급·검증할 수 있는 토큰 버전입니다.
const SupportedVersion = 1

// Sign은 Payload를 Ed25519 개인키로 서명하여 토큰 문자열을 반환합니다.
//
// 출력: <base64url(json-payload)>.<base64url(ed25519-signature)>
// payload-json은 Go encoding/json 기본 직렬화 — 키 정렬·canonical 형식은 후속 (현재는 round-trip 신뢰).
func Sign(priv ed25519.PrivateKey, p Payload) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("license: invalid private key size %d", len(priv))
	}
	if p.Version == 0 {
		p.Version = SupportedVersion
	}
	if p.Version != SupportedVersion {
		return "", ErrUnsupportedVer
	}
	jsonBytes, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("license: marshal payload: %w", err)
	}
	sig := ed25519.Sign(priv, jsonBytes)

	enc := base64.RawURLEncoding
	return enc.EncodeToString(jsonBytes) + "." + enc.EncodeToString(sig), nil
}

// Verify는 토큰을 Ed25519 공개키로 검증하고 Payload를 반환합니다.
//
// 검증 단계: format → base64 decode → JSON decode → version check → signature verify.
// 만료 검사는 IsExpired로 분리 (호출자가 시점 결정).
func Verify(pub ed25519.PublicKey, token string) (Payload, error) {
	if len(pub) != ed25519.PublicKeySize {
		return Payload{}, fmt.Errorf("license: invalid public key size %d", len(pub))
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Payload{}, ErrInvalidFormat
	}
	enc := base64.RawURLEncoding
	jsonBytes, err := enc.DecodeString(parts[0])
	if err != nil {
		return Payload{}, ErrInvalidFormat
	}
	sig, err := enc.DecodeString(parts[1])
	if err != nil {
		return Payload{}, ErrInvalidFormat
	}

	var p Payload
	if err := json.Unmarshal(jsonBytes, &p); err != nil {
		return Payload{}, ErrInvalidPayload
	}
	if p.Version != SupportedVersion {
		return Payload{}, ErrUnsupportedVer
	}
	if !ed25519.Verify(pub, jsonBytes, sig) {
		return Payload{}, ErrInvalidSignature
	}
	return p, nil
}
