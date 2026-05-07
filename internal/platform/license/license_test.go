package license_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/license"
)

// generateKey는 테스트용 Ed25519 키 페어를 생성합니다.
func generateKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

// fixedNow는 결정론적 시점 주입용.
func fixedNow(ts string) func() time.Time {
	t, _ := time.Parse(time.RFC3339, ts)
	return func() time.Time { return t }
}

func samplePayload() license.Payload {
	return license.Payload{
		Version:   license.SupportedVersion,
		LicenseID: "lic_TEST",
		IssuedTo:  "Acme Corp",
		IssuedAt:  mustTime("2026-01-01T00:00:00Z"),
		ExpiresAt: mustTime("2027-01-01T00:00:00Z"),
		Edition:   license.EditionEnterprise,
		Features:  []license.Feature{license.FeatureSSO, license.FeatureWebhook},
		Quotas: license.Quota{
			RobotsMax:       100,
			ScansPerDay:     1000,
			LLMTokensPerDay: 1_000_000,
		},
	}
}

func mustTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// === Sign / Verify ===

func TestSignAndVerifyRoundTrip(t *testing.T) {
	pub, priv := generateKey(t)
	p := samplePayload()

	tok, err := license.Sign(priv, p)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !strings.Contains(tok, ".") {
		t.Errorf("token missing '.': %q", tok)
	}

	out, err := license.Verify(pub, tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.LicenseID != p.LicenseID {
		t.Errorf("LicenseID = %q, want %q", out.LicenseID, p.LicenseID)
	}
	if out.Edition != license.EditionEnterprise {
		t.Errorf("Edition = %q, want enterprise", out.Edition)
	}
	if !out.HasFeature(license.FeatureSSO) {
		t.Error("HasFeature(SSO) = false")
	}
	if out.HasFeature(license.FeatureCloud) {
		t.Error("HasFeature(Cloud) = true, want false (not licensed)")
	}
}

func TestVerifyRejectsTamperedSignature(t *testing.T) {
	pub, priv := generateKey(t)
	tok, _ := license.Sign(priv, samplePayload())

	// payload는 그대로, signature 첫 char를 변조 (RawURL base64는 padding 없어 마지막
	// char의 일부 비트는 zero-pad이라 변경해도 디코드 결과가 동일할 수 있음).
	parts := strings.Split(tok, ".")
	if len(parts) != 2 {
		t.Fatalf("token format invalid")
	}
	tampered := parts[0] + "." + flipFirstChar(parts[1])

	_, err := license.Verify(pub, tampered)
	if !errors.Is(err, license.ErrInvalidSignature) {
		t.Errorf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsWrongPublicKey(t *testing.T) {
	_, priv := generateKey(t)
	otherPub, _ := generateKey(t)
	tok, _ := license.Sign(priv, samplePayload())

	_, err := license.Verify(otherPub, tok)
	if !errors.Is(err, license.ErrInvalidSignature) {
		t.Errorf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsMalformedToken(t *testing.T) {
	pub, _ := generateKey(t)
	cases := []string{
		"",
		"single-segment",
		"a.b.c", // 3 segment
		"!@#.@#$",
	}
	for _, tok := range cases {
		_, err := license.Verify(pub, tok)
		if err == nil {
			t.Errorf("Verify(%q) want error, got nil", tok)
		}
	}
}

func TestVerifyRejectsUnsupportedVersion(t *testing.T) {
	pub, priv := generateKey(t)
	p := samplePayload()
	p.Version = 99
	// Sign은 99→error 반환할 텐데, 우회를 위해 직접 marshal.
	// 본 테스트는 Verify의 version 분기만 — Sign을 통한 round-trip은 불가하므로
	// 대체 방법: Sign이 ErrUnsupportedVer 반환 검증.
	_, err := license.Sign(priv, p)
	if !errors.Is(err, license.ErrUnsupportedVer) {
		t.Errorf("Sign ver=99 err = %v, want ErrUnsupportedVer", err)
	}
	// pub 파라미터는 시그니처 형식에 필요 — 사용 안 됨.
	_ = pub
}

func TestSignDefaultsVersionTo1(t *testing.T) {
	_, priv := generateKey(t)
	p := samplePayload()
	p.Version = 0
	tok, err := license.Sign(priv, p)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if tok == "" {
		t.Error("empty token")
	}
}

// === Payload helpers ===

func TestIsExpired(t *testing.T) {
	p := samplePayload()
	if p.IsExpired(mustTime("2026-06-01T00:00:00Z")) {
		t.Error("IsExpired(mid-life) = true, want false")
	}
	if !p.IsExpired(mustTime("2027-06-01T00:00:00Z")) {
		t.Error("IsExpired(after expiry) = false, want true")
	}
}

func TestQuotaIsUnlimited(t *testing.T) {
	q := license.Quota{RobotsMax: 0, ScansPerDay: -1, LLMTokensPerDay: 100}
	if !q.IsUnlimited("robots_max") {
		t.Error("RobotsMax=0 should be unlimited")
	}
	if !q.IsUnlimited("scans_per_day") {
		t.Error("ScansPerDay=-1 should be unlimited")
	}
	if q.IsUnlimited("llm_tokens_per_day") {
		t.Error("LLMTokens=100 should NOT be unlimited")
	}
	if q.IsUnlimited("unknown_field") {
		t.Error("unknown_field should default to false")
	}
}

// === Enforcer / Quota ===

type stubUsage struct {
	robots, scans, tokens int
	err                   error
}

func (s *stubUsage) CurrentRobots(_ context.Context, _ string) (int, error) {
	return s.robots, s.err
}
func (s *stubUsage) ScansToday(_ context.Context, _ string) (int, error) {
	return s.scans, s.err
}
func (s *stubUsage) LLMTokensToday(_ context.Context, _ string) (int, error) {
	return s.tokens, s.err
}

func TestCheckFeatureAllowsLicensedFeature(t *testing.T) {
	e := license.NewEnforcer(samplePayload(), &stubUsage{}, fixedNow("2026-06-01T00:00:00Z"))
	r := e.CheckFeature(license.FeatureSSO)
	if !r.Allowed {
		t.Errorf("SSO denied: %s", r.Reason)
	}
}

func TestCheckFeatureRejectsUnlicensedFeature(t *testing.T) {
	e := license.NewEnforcer(samplePayload(), &stubUsage{}, fixedNow("2026-06-01T00:00:00Z"))
	r := e.CheckFeature(license.FeatureCloud)
	if r.Allowed {
		t.Error("Cloud allowed without license")
	}
}

func TestCheckFeatureRejectsExpiredLicense(t *testing.T) {
	e := license.NewEnforcer(samplePayload(), &stubUsage{}, fixedNow("2028-01-01T00:00:00Z"))
	r := e.CheckFeature(license.FeatureSSO)
	if r.Allowed {
		t.Error("SSO allowed despite expired license")
	}
	if !strings.Contains(r.Reason, "expired") {
		t.Errorf("reason=%q, want 'expired'", r.Reason)
	}
}

func TestCheckFeatureRejectsCommunityEdition(t *testing.T) {
	p := samplePayload()
	p.Edition = license.EditionCommunity
	e := license.NewEnforcer(p, &stubUsage{}, fixedNow("2026-06-01T00:00:00Z"))
	r := e.CheckFeature(license.FeatureSSO)
	if r.Allowed {
		t.Error("Community edition allowed enterprise feature")
	}
}

func TestCheckFeatureRejectsNoLicense(t *testing.T) {
	e := license.NewEnforcer(license.Payload{}, &stubUsage{}, fixedNow("2026-06-01T00:00:00Z"))
	r := e.CheckFeature(license.FeatureSSO)
	if r.Allowed {
		t.Error("No license allowed enterprise feature")
	}
}

func TestCheckRobotsAddRespectsQuota(t *testing.T) {
	e := license.NewEnforcer(samplePayload(), &stubUsage{robots: 99}, fixedNow("2026-06-01T00:00:00Z"))
	// 99 + 1 = 100 (max=100) → 통과.
	r, err := e.CheckRobotsAdd(context.Background(), "tn_x", 1)
	if err != nil {
		t.Fatalf("CheckRobotsAdd: %v", err)
	}
	if !r.Allowed {
		t.Errorf("100/100 should be allowed, reason=%s", r.Reason)
	}
	// 99 + 2 = 101 → 거부.
	r2, _ := e.CheckRobotsAdd(context.Background(), "tn_x", 2)
	if r2.Allowed {
		t.Error("101/100 should be denied")
	}
	if r2.Field != "robots_max" {
		t.Errorf("Field = %q, want robots_max", r2.Field)
	}
}

func TestCheckRobotsAddSkippedForUnlimited(t *testing.T) {
	p := samplePayload()
	p.Quotas.RobotsMax = 0 // unlimited
	e := license.NewEnforcer(p, &stubUsage{robots: 100000}, fixedNow("2026-06-01T00:00:00Z"))
	r, _ := e.CheckRobotsAdd(context.Background(), "tn_x", 100)
	if !r.Allowed {
		t.Errorf("unlimited should always allow, reason=%s", r.Reason)
	}
}

func TestCheckScansTodayQuota(t *testing.T) {
	e := license.NewEnforcer(samplePayload(), &stubUsage{scans: 999}, fixedNow("2026-06-01T00:00:00Z"))
	r, _ := e.CheckScansToday(context.Background(), "tn_x")
	if !r.Allowed {
		t.Error("999+1=1000/1000 should be allowed")
	}
	e2 := license.NewEnforcer(samplePayload(), &stubUsage{scans: 1000}, fixedNow("2026-06-01T00:00:00Z"))
	r2, _ := e2.CheckScansToday(context.Background(), "tn_x")
	if r2.Allowed {
		t.Error("1001/1000 should be denied")
	}
}

func TestCheckLLMTokensQuota(t *testing.T) {
	e := license.NewEnforcer(samplePayload(), &stubUsage{tokens: 900_000}, fixedNow("2026-06-01T00:00:00Z"))
	r, _ := e.CheckLLMTokens(context.Background(), "tn_x", 100_000)
	if !r.Allowed {
		t.Error("1M/1M should be allowed")
	}
	r2, _ := e.CheckLLMTokens(context.Background(), "tn_x", 100_001)
	if r2.Allowed {
		t.Error("1M+1 should be denied")
	}
}

func TestNoLicenseAllowsRobotAddByDefault(t *testing.T) {
	// 라이선스가 없으면 community SKU — 한도는 도메인이 결정. 본 모듈은 통과.
	e := license.NewEnforcer(license.Payload{}, &stubUsage{robots: 100000}, fixedNow("2026-06-01T00:00:00Z"))
	r, _ := e.CheckRobotsAdd(context.Background(), "tn_x", 1)
	if !r.Allowed {
		t.Error("no license should allow (community defaults)")
	}
}

func flipFirstChar(s string) string {
	if s == "" {
		return s
	}
	first := s[0]
	flip := byte('A')
	if first == 'A' {
		flip = 'B'
	}
	return string(flip) + s[1:]
}
