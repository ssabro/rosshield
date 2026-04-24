package tenant_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/tenant"
)

func newJWTKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func TestSignAndParseAccessTokenRoundtrip(t *testing.T) {
	t.Parallel()
	pub, priv := newJWTKeyPair(t)

	now := time.Now().UTC().Truncate(time.Second)
	in := tenant.AccessClaims{
		Subject:   "us_abc",
		TenantID:  "tn_acme",
		Roles:     []string{"admin", "auditor"},
		IssuedAt:  now,
		ExpiresAt: now.Add(15 * time.Minute),
		JTI:       "at_xyz",
	}
	token, err := tenant.SignAccessToken(priv, in)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	out, err := tenant.ParseAccessToken(pub, token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if out.Subject != in.Subject || out.TenantID != in.TenantID || out.JTI != in.JTI {
		t.Errorf("claims mismatch: got %+v, want %+v", out, in)
	}
	if !out.ExpiresAt.Equal(in.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", out.ExpiresAt, in.ExpiresAt)
	}
	if len(out.Roles) != 2 || out.Roles[0] != "admin" || out.Roles[1] != "auditor" {
		t.Errorf("Roles = %v, want [admin auditor]", out.Roles)
	}
}

// E3.T4 본체.
func TestParseAccessTokenRejectsExpiredAndBadSig(t *testing.T) {
	t.Parallel()
	pub, priv := newJWTKeyPair(t)

	// 1) 만료 토큰 — 1시간 전 발급/만료.
	past := time.Now().UTC().Add(-1 * time.Hour)
	expired, err := tenant.SignAccessToken(priv, tenant.AccessClaims{
		Subject: "us_x", TenantID: "tn_x",
		IssuedAt: past.Add(-15 * time.Minute), ExpiresAt: past,
	})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	_, err = tenant.ParseAccessToken(pub, expired)
	if !errors.Is(err, tenant.ErrTokenExpired) {
		t.Errorf("expired: err = %v, want ErrTokenExpired", err)
	}

	// 2) 다른 키로 검증 → 시그니처 불일치.
	otherPub, _ := newJWTKeyPair(t)
	good, _ := tenant.SignAccessToken(priv, tenant.AccessClaims{
		Subject: "us_x", TenantID: "tn_x",
		IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	})
	_, err = tenant.ParseAccessToken(otherPub, good)
	if !errors.Is(err, tenant.ErrTokenSignatureInvalid) {
		t.Errorf("wrong key: err = %v, want ErrTokenSignatureInvalid", err)
	}

	// 3) 변조된 token — signature 첫 글자 변경 (마지막 byte는 base64url의 unused bit일 수 있음).
	parts := strings.Split(good, ".")
	if len(parts) != 3 {
		t.Fatalf("token format unexpected: %d parts", len(parts))
	}
	sig := parts[2]
	flipped := "A"
	if sig[0] == 'A' {
		flipped = "B"
	}
	tampered := parts[0] + "." + parts[1] + "." + flipped + sig[1:]
	_, err = tenant.ParseAccessToken(pub, tampered)
	if err == nil {
		t.Error("tampered token: expected error")
	}

	// 4) 빈 토큰 → ErrInvalidToken.
	_, err = tenant.ParseAccessToken(pub, "")
	if !errors.Is(err, tenant.ErrInvalidToken) {
		t.Errorf("empty token: err = %v, want ErrInvalidToken", err)
	}
}

// alg 혼동·alg=none 차단 검증 (jwt.WithValidMethods).
func TestParseAccessTokenRejectsAlgNone(t *testing.T) {
	t.Parallel()
	pub, _ := newJWTKeyPair(t)

	// alg=none JWT — header `eyJhbGciOiJub25lIn0` (`{"alg":"none"}`) + payload + 빈 sig.
	noneToken := "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ1c194In0."
	_, err := tenant.ParseAccessToken(pub, noneToken)
	if err == nil {
		t.Fatal("alg=none should be rejected")
	}
	// jwt v5는 alg 불일치를 ErrTokenSignatureInvalid로 매핑 — 도메인 sentinel은 둘 다 거부 의미.
	if !errors.Is(err, tenant.ErrTokenSignatureInvalid) && !errors.Is(err, tenant.ErrInvalidToken) {
		t.Errorf("alg=none: err = %v, want ErrTokenSignatureInvalid or ErrInvalidToken", err)
	}
}

func TestSignAndParseRefreshToken(t *testing.T) {
	t.Parallel()
	pub, priv := newJWTKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)

	in := tenant.RefreshClaims{
		Subject:   "us_abc",
		TenantID:  "tn_acme",
		IssuedAt:  now,
		ExpiresAt: now.Add(14 * 24 * time.Hour),
		JTI:       "rt_xyz",
	}
	tok, err := tenant.SignRefreshToken(priv, in)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !strings.Contains(tok, ".") {
		t.Error("token should contain dots (JWS compact)")
	}

	out, err := tenant.ParseRefreshToken(pub, tok)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if out.JTI != in.JTI || out.TenantID != in.TenantID {
		t.Errorf("claims mismatch: %+v", out)
	}
}
