package sso_test

// oidc_test.go — E20-B OIDC 단위 테스트.
//
// 검증 포인트(8건+):
//
//	1. ParseOIDCConfig: 정상 + 누락 필드 거부.
//	2. Discover: well-known 응답 + 비-2xx 에러 + issuer mismatch 거부.
//	3. BuildAuthURL: 표준 query string + PKCE S256 challenge + nonce·state 포함.
//	4. ExchangeCode: token_endpoint POST 폼 형식 + id_token 추출.
//	5. VerifyIDToken: RS256 서명 검증 + iss/aud/exp/nonce 매칭 + 만료 거부.
//	6. VerifyIDToken: nonce mismatch 거부.
//	7. VerifyIDToken: aud mismatch 거부.
//	8. VerifyIDToken: 미서명 알고리즘(none) 거부.
//	9. PKCE S256 변환 결정성.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
)

// === mock IdP ===
//
// httptest.Server로 OIDC IdP를 시뮬레이션:
//
//	GET /.well-known/openid-configuration → discovery JSON (현재 server URL 기반).
//	GET /jwks                              → JWKS (RSA pub key).
//	POST /token                            → tokenResponse(서명된 id_token + access_token).
//	GET /authorize                         → 본 테스트는 직접 호출 X (BuildAuthURL 결과 검증만).

type mockIdP struct {
	t       *testing.T
	server  *httptest.Server
	priv    *rsa.PrivateKey
	pub     *rsa.PublicKey
	kid     string
	clockFn func() time.Time

	// 발급 시 적용할 claim override — 테스트가 잘못된 nonce/aud/exp 시나리오 주입.
	overrideClaims func(jwt.MapClaims)
}

func newMockIdP(t *testing.T, clockFn func() time.Time) *mockIdP {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	idp := &mockIdP{t: t, priv: priv, pub: &priv.PublicKey, kid: "test-key-1", clockFn: clockFn}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", idp.handleDiscovery)
	mux.HandleFunc("/jwks", idp.handleJWKS)
	mux.HandleFunc("/token", idp.handleToken)
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (m *mockIdP) issuer() string { return m.server.URL }

func (m *mockIdP) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	d := sso.OIDCDiscovery{
		Issuer:                m.issuer(),
		AuthorizationEndpoint: m.issuer() + "/authorize",
		TokenEndpoint:         m.issuer() + "/token",
		JWKSURI:               m.issuer() + "/jwks",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(d)
}

func (m *mockIdP) handleJWKS(w http.ResponseWriter, r *http.Request) {
	n := base64.RawURLEncoding.EncodeToString(m.pub.N.Bytes())
	// e=65537 → big-endian {0x01,0x00,0x01}
	e := base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01})
	body := map[string]any{
		"keys": []map[string]any{
			{"kty": "RSA", "kid": m.kid, "use": "sig", "alg": "RS256", "n": n, "e": e},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// handleToken은 form 검증 후 서명된 id_token을 반환합니다.
//
// form 검증:
//
//	grant_type == authorization_code
//	code_verifier present
//	client_id present
func (m *mockIdP) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if r.Form.Get("grant_type") != "authorization_code" {
		http.Error(w, "wrong grant_type", http.StatusBadRequest)
		return
	}
	if r.Form.Get("code_verifier") == "" {
		http.Error(w, "missing code_verifier", http.StatusBadRequest)
		return
	}
	clientID := r.Form.Get("client_id")
	if clientID == "" {
		http.Error(w, "missing client_id", http.StatusBadRequest)
		return
	}

	now := m.clockFn().UTC()
	claims := jwt.MapClaims{
		"iss":            m.issuer(),
		"aud":            clientID,
		"sub":            "user-12345",
		"email":          "alice@example.test",
		"email_verified": true,
		"name":           "Alice",
		"nonce":          "expected-nonce-abcdef",
		"iat":            now.Unix(),
		"exp":            now.Add(5 * time.Minute).Unix(),
	}
	if m.overrideClaims != nil {
		m.overrideClaims(claims)
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = m.kid
	signed, err := tok.SignedString(m.priv)
	if err != nil {
		http.Error(w, "sign error", http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"access_token": "fake-access-token-xyz",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     signed,
		"scope":        "openid email profile",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// === fixture helper ===

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func mkClient(now func() time.Time) *sso.OIDCClient {
	return &sso.OIDCClient{
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		Now:        now,
		Leeway:     30 * time.Second,
	}
}

// === tests ===

func TestParseOIDCConfig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"full", `{"issuer":"https://x","clientId":"c","redirectUri":"https://app/cb"}`, true},
		{"missing issuer", `{"clientId":"c","redirectUri":"https://app/cb"}`, false},
		{"missing clientId", `{"issuer":"https://x","redirectUri":"https://app/cb"}`, false},
		{"missing redirectUri", `{"issuer":"https://x","clientId":"c"}`, false},
		{"empty json", `{}`, false},
		{"empty raw", ``, false},
		{"malformed", `{not json}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := sso.ParseOIDCConfig(json.RawMessage(tc.in))
			if tc.ok {
				if err != nil {
					t.Errorf("ParseOIDCConfig: unexpected err: %v", err)
				}
				if len(cfg.Scopes) == 0 {
					t.Errorf("Scopes default not applied")
				}
				return
			}
			if err == nil {
				t.Errorf("expected error, got cfg=%+v", cfg)
			}
		})
	}
}

func TestDiscover(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMockIdP(t, fixedClock(now))
	c := mkClient(fixedClock(now))

	d, err := c.Discover(context.Background(), idp.issuer())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if d.Issuer != idp.issuer() {
		t.Errorf("Issuer = %q, want %q", d.Issuer, idp.issuer())
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.JWKSURI == "" {
		t.Errorf("missing endpoints: %+v", d)
	}
}

func TestDiscoverIssuerMismatch(t *testing.T) {
	t.Parallel()
	// 잘못된 issuer를 반환하는 mock 서버.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 "https://different-issuer.example",
			"authorization_endpoint": "https://x/authorize",
			"token_endpoint":         "https://x/token",
			"jwks_uri":               "https://x/jwks",
		})
	}))
	defer srv.Close()

	c := mkClient(time.Now)
	_, err := c.Discover(context.Background(), srv.URL)
	if !errors.Is(err, sso.ErrIdPHTTP) {
		t.Errorf("Discover mismatch: err = %v, want ErrIdPHTTP", err)
	}
}

func TestDiscover404(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := mkClient(time.Now)
	_, err := c.Discover(context.Background(), srv.URL)
	if !errors.Is(err, sso.ErrIdPHTTP) {
		t.Errorf("404: err = %v, want ErrIdPHTTP", err)
	}
}

func TestBuildAuthURL(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMockIdP(t, fixedClock(now))
	c := mkClient(fixedClock(now))

	cfg := sso.OIDCConfig{
		Issuer:      idp.issuer(),
		ClientID:    "client-abc",
		RedirectURI: "https://app/callback",
		Scopes:      []string{"openid", "email"},
	}
	authURL, err := c.BuildAuthURL(context.Background(), cfg, "state-XYZ", "verifier-VVV", "nonce-NNN")
	if err != nil {
		t.Fatalf("BuildAuthURL: %v", err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.HasPrefix(authURL, idp.issuer()+"/authorize") {
		t.Errorf("authURL = %q, want prefix /authorize", authURL)
	}
	q := u.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("client_id") != "client-abc" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "https://app/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("scope") != "openid email" {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	if q.Get("state") != "state-XYZ" {
		t.Errorf("state = %q", q.Get("state"))
	}
	if q.Get("nonce") != "nonce-NNN" {
		t.Errorf("nonce = %q", q.Get("nonce"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", q.Get("code_challenge_method"))
	}
	// PKCE S256 verifier 검증 — base64url(sha256("verifier-VVV"))
	h := sha256.Sum256([]byte("verifier-VVV"))
	want := base64.RawURLEncoding.EncodeToString(h[:])
	if q.Get("code_challenge") != want {
		t.Errorf("code_challenge = %q, want %q", q.Get("code_challenge"), want)
	}
}

func TestBuildAuthURLRejectsEmptyArgs(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMockIdP(t, fixedClock(now))
	c := mkClient(fixedClock(now))
	cfg := sso.OIDCConfig{Issuer: idp.issuer(), ClientID: "c", RedirectURI: "https://x/cb", Scopes: []string{"openid"}}

	if _, err := c.BuildAuthURL(context.Background(), cfg, "", "v", "n"); !errors.Is(err, sso.ErrInvalidOIDCArgs) {
		t.Errorf("empty state: err = %v", err)
	}
	if _, err := c.BuildAuthURL(context.Background(), cfg, "s", "", "n"); !errors.Is(err, sso.ErrInvalidOIDCArgs) {
		t.Errorf("empty verifier: err = %v", err)
	}
	if _, err := c.BuildAuthURL(context.Background(), cfg, "s", "v", ""); !errors.Is(err, sso.ErrInvalidOIDCArgs) {
		t.Errorf("empty nonce: err = %v", err)
	}
}

func TestExchangeCode(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMockIdP(t, fixedClock(now))
	c := mkClient(fixedClock(now))

	cfg := sso.OIDCConfig{
		Issuer:      idp.issuer(),
		ClientID:    "client-abc",
		RedirectURI: "https://app/callback",
		Scopes:      []string{"openid", "email"},
	}
	idTok, accessTok, err := c.ExchangeCode(context.Background(), cfg, "auth-code-123", "verifier-VVV")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if idTok == "" {
		t.Errorf("idToken empty")
	}
	if accessTok != "fake-access-token-xyz" {
		t.Errorf("accessToken = %q", accessTok)
	}
	// id_token이 JWT 3-part 형식인지 빠르게 확인.
	if parts := strings.Split(idTok, "."); len(parts) != 3 {
		t.Errorf("id_token not 3-part JWT: %s", idTok)
	}
}

func TestExchangeCodeIdPError(t *testing.T) {
	t.Parallel()
	// /token이 500을 반환하는 mock 서버.
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		// dummy issuer는 mock 서버 자신.
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/.well-known/openid-configuration/", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{}})
	})
	// discovery는 직접 만들어 등록.
	mux2 := http.NewServeMux()
	mux2.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
			"jwks_uri":               srv.URL + "/jwks",
		})
	})
	mux2.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv2 := httptest.NewServer(mux2)
	defer srv2.Close()

	c := mkClient(time.Now)
	cfg := sso.OIDCConfig{Issuer: srv2.URL, ClientID: "c", RedirectURI: "https://x/cb", Scopes: []string{"openid"}}
	_, _, err := c.ExchangeCode(context.Background(), cfg, "code", "verifier")
	if !errors.Is(err, sso.ErrIdPHTTP) {
		t.Errorf("token 500: err = %v, want ErrIdPHTTP", err)
	}
}

func TestVerifyIDTokenSuccess(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMockIdP(t, fixedClock(now))
	c := mkClient(fixedClock(now))

	cfg := sso.OIDCConfig{
		Issuer:      idp.issuer(),
		ClientID:    "client-abc",
		RedirectURI: "https://app/callback",
		Scopes:      []string{"openid"},
	}

	// 정상 흐름 — ExchangeCode로 id_token 받아 검증.
	idTok, _, err := c.ExchangeCode(context.Background(), cfg, "code", "verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	claims, err := c.VerifyIDToken(context.Background(), cfg, idTok, "expected-nonce-abcdef")
	if err != nil {
		t.Fatalf("VerifyIDToken: %v", err)
	}
	if claims.Subject != "user-12345" {
		t.Errorf("Subject = %q", claims.Subject)
	}
	if claims.Email != "alice@example.test" {
		t.Errorf("Email = %q", claims.Email)
	}
	if !claims.EmailVerified {
		t.Errorf("EmailVerified = false, want true")
	}
	if len(claims.Audience) == 0 || claims.Audience[0] != "client-abc" {
		t.Errorf("Audience = %v", claims.Audience)
	}
}

func TestVerifyIDTokenNonceMismatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMockIdP(t, fixedClock(now))
	c := mkClient(fixedClock(now))
	cfg := sso.OIDCConfig{Issuer: idp.issuer(), ClientID: "client-abc", RedirectURI: "https://x/cb", Scopes: []string{"openid"}}

	idTok, _, err := c.ExchangeCode(context.Background(), cfg, "code", "verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	_, err = c.VerifyIDToken(context.Background(), cfg, idTok, "WRONG-nonce")
	if !errors.Is(err, sso.ErrNonceMismatch) {
		t.Errorf("nonce mismatch err = %v, want ErrNonceMismatch", err)
	}
}

func TestVerifyIDTokenAudienceMismatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMockIdP(t, fixedClock(now))
	idp.overrideClaims = func(c jwt.MapClaims) {
		c["aud"] = "DIFFERENT-CLIENT"
	}
	c := mkClient(fixedClock(now))
	cfg := sso.OIDCConfig{Issuer: idp.issuer(), ClientID: "client-abc", RedirectURI: "https://x/cb", Scopes: []string{"openid"}}

	idTok, _, err := c.ExchangeCode(context.Background(), cfg, "code", "verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	_, err = c.VerifyIDToken(context.Background(), cfg, idTok, "expected-nonce-abcdef")
	if !errors.Is(err, sso.ErrIDTokenInvalid) {
		t.Errorf("aud mismatch err = %v, want ErrIDTokenInvalid", err)
	}
}

func TestVerifyIDTokenExpired(t *testing.T) {
	t.Parallel()
	issuedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMockIdP(t, fixedClock(issuedAt))

	// IdP는 issuedAt+5분으로 발급, 검증은 그 후 1시간 시점.
	verifyAt := issuedAt.Add(time.Hour)
	c := mkClient(fixedClock(verifyAt))
	cfg := sso.OIDCConfig{Issuer: idp.issuer(), ClientID: "client-abc", RedirectURI: "https://x/cb", Scopes: []string{"openid"}}

	idTok, _, err := c.ExchangeCode(context.Background(), cfg, "code", "verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	_, err = c.VerifyIDToken(context.Background(), cfg, idTok, "expected-nonce-abcdef")
	if !errors.Is(err, sso.ErrIDTokenInvalid) {
		t.Errorf("expired err = %v, want ErrIDTokenInvalid", err)
	}
}

func TestVerifyIDTokenIssuerMismatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMockIdP(t, fixedClock(now))
	idp.overrideClaims = func(c jwt.MapClaims) {
		c["iss"] = "https://attacker.example"
	}
	c := mkClient(fixedClock(now))
	cfg := sso.OIDCConfig{Issuer: idp.issuer(), ClientID: "client-abc", RedirectURI: "https://x/cb", Scopes: []string{"openid"}}

	idTok, _, err := c.ExchangeCode(context.Background(), cfg, "code", "verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	_, err = c.VerifyIDToken(context.Background(), cfg, idTok, "expected-nonce-abcdef")
	if !errors.Is(err, sso.ErrIDTokenInvalid) {
		t.Errorf("iss mismatch err = %v, want ErrIDTokenInvalid", err)
	}
}

func TestVerifyIDTokenRejectsNoneAlg(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMockIdP(t, fixedClock(now))
	c := mkClient(fixedClock(now))
	cfg := sso.OIDCConfig{Issuer: idp.issuer(), ClientID: "client-abc", RedirectURI: "https://x/cb", Scopes: []string{"openid"}}

	// "none" 알고리즘 토큰을 직접 만든다 (서명 X).
	claims := jwt.MapClaims{
		"iss":   idp.issuer(),
		"aud":   "client-abc",
		"sub":   "user-12345",
		"nonce": "expected-nonce-abcdef",
		"iat":   now.Unix(),
		"exp":   now.Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	tok.Header["kid"] = idp.kid
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}
	_, err = c.VerifyIDToken(context.Background(), cfg, signed, "expected-nonce-abcdef")
	if !errors.Is(err, sso.ErrIDTokenInvalid) {
		t.Errorf("none alg err = %v, want ErrIDTokenInvalid", err)
	}
}

func TestPKCEDeterminism(t *testing.T) {
	t.Parallel()
	// 같은 verifier는 항상 같은 challenge — RFC 7636 §4.2.
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMockIdP(t, fixedClock(now))
	c := mkClient(fixedClock(now))
	cfg := sso.OIDCConfig{Issuer: idp.issuer(), ClientID: "c", RedirectURI: "https://x/cb", Scopes: []string{"openid"}}

	url1, err := c.BuildAuthURL(context.Background(), cfg, "s1", "same-verifier", "n1")
	if err != nil {
		t.Fatalf("build1: %v", err)
	}
	url2, err := c.BuildAuthURL(context.Background(), cfg, "s2", "same-verifier", "n2")
	if err != nil {
		t.Fatalf("build2: %v", err)
	}
	q1, _ := url.Parse(url1)
	q2, _ := url.Parse(url2)
	if q1.Query().Get("code_challenge") != q2.Query().Get("code_challenge") {
		t.Errorf("PKCE challenge non-deterministic: %q vs %q",
			q1.Query().Get("code_challenge"), q2.Query().Get("code_challenge"))
	}
}

// (no extra helpers — keep oidc_test.go scoped to public surface checks.)
