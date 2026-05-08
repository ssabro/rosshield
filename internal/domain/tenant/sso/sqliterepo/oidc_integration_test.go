package sqliterepo_test

// oidc_integration_test.go — E20-B sqliterepo + OIDCClient 통합 테스트.
//
// 본 파일은 mock IdP(httptest.Server)를 띄우고 sqliterepo.Repo에 *sso.OIDCClient를 주입하여:
//
//	1. StartLogin → AuthURL이 빈 값이 아니라 실제 mock IdP authorization endpoint 가리키는지.
//	2. CompleteLogin → mock IdP /token POST → id_token 검증 → ExternalIdentity 채워지는지.
//	3. IdentityResolver를 통한 user.ID 매핑.
//
// repo_test.go의 fakes(stepClock·fakeAuditEmitter·newTestHarness)를 재사용.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ssabro/rosshield/internal/domain/tenant/sso"
	"github.com/ssabro/rosshield/internal/domain/tenant/sso/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// === local mini-IdP (테스트 패키지 격리) ===
//
// oidc_test.go의 mockIdP과 같은 형태지만 본 패키지(sqliterepo_test)에 격리.
// 외부 패키지의 unexported 사용 불가하므로 별도 helper.

type miniIdP struct {
	srv     *httptest.Server
	priv    *rsa.PrivateKey
	pub     *rsa.PublicKey
	kid     string
	now     func() time.Time
	subject string // 발급 id_token의 sub (default: "user-12345")
	email   string // 발급 id_token의 email (default: "alice@example.test")
}

func newMiniIdP(t *testing.T, now func() time.Time) *miniIdP {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	idp := &miniIdP{
		priv: priv, pub: &priv.PublicKey, kid: "mini-key-1",
		now:     now,
		subject: "user-12345",
		email:   "alice@example.test",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 idp.srv.URL,
			"authorization_endpoint": idp.srv.URL + "/authorize",
			"token_endpoint":         idp.srv.URL + "/token",
			"jwks_uri":               idp.srv.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(idp.pub.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{"kty": "RSA", "kid": idp.kid, "alg": "RS256", "use": "sig", "n": n, "e": e},
			},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		clientID := r.Form.Get("client_id")
		// nonce를 attempt에 일치시키려면 본 mock에서는 token이 받은 client_id 기반으로 nonce 결정 X —
		// 별도 nonceProvider로 client에서 명시. 본 테스트는 attempt.Nonce를 known하게 추출 후 mock에 set.
		now := idp.now().UTC()
		claims := jwt.MapClaims{
			"iss":            idp.srv.URL,
			"aud":            clientID,
			"sub":            idp.subject,
			"email":          idp.email,
			"email_verified": true,
			"nonce":          idp.fixedNonce(),
			"iat":            now.Unix(),
			"exp":            now.Add(5 * time.Minute).Unix(),
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = idp.kid
		signed, _ := tok.SignedString(idp.priv)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-access",
			"token_type":   "Bearer",
			"id_token":     signed,
		})
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

// fixedNonce는 mock이 발급할 nonce — 본 테스트는 mock id_token nonce를 강제 합치시키기 위해
// StartLogin 후 attempt.Nonce를 mock에 setNonce로 주입.
var miniIdPNonce = "mock-nonce-must-match"

func (m *miniIdP) fixedNonce() string { return miniIdPNonce }

// === IdentityResolver fake ===

type fakeIdentityResolver struct {
	called bool
	uid    string
}

func (f *fakeIdentityResolver) ResolveOIDCIdentity(_ context.Context, _ storage.Tx, _ storage.TenantID, _ string, _ sso.IDTokenClaims) (string, error) {
	f.called = true
	return f.uid, nil
}

func (f *fakeIdentityResolver) ResolveSAMLIdentity(_ context.Context, _ storage.Tx, _ storage.TenantID, _ string, _ sso.SAMLAssertion) (string, error) {
	f.called = true
	return f.uid, nil
}

// === harness with OIDC ===
//
// repo_test.go의 newTestHarness가 OIDC 미주입 — 본 함수는 같은 스토리지 셋업 + OIDC + Resolver 주입.
// 코드 중복을 피하려고 newTestHarness에 옵션을 넣을 수 있지만, 본 stage는 분리 유지.

func newOIDCHarness(t *testing.T, idp *miniIdP, resolver sqliterepo.IdentityResolver) *harness {
	t.Helper()
	h := newTestHarness(t)

	// OIDC client를 주입한 새 Repo로 교체.
	oidcClient := &sso.OIDCClient{
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		Now:        h.clock.Now,
		Leeway:     30 * time.Second,
	}
	h.repo = sqliterepo.New(sqliterepo.Deps{
		Clock:            h.clock,
		IDGen:            idgen.NewULID(),
		Audit:            h.emitter,
		OIDC:             oidcClient,
		IdentityResolver: resolver,
	})
	return h
}

func TestStartLoginOIDCWithClientReturnsAuthURL(t *testing.T) {
	// 본 테스트는 miniIdPNonce 글로벌 변수를 건들지 않음 — t.Parallel() OK.
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMiniIdP(t, fixedNow(now))
	h := newOIDCHarness(t, idp, nil)

	cfg := fmt.Sprintf(`{"issuer":"%s","clientId":"client-XYZ","redirectUri":"https://app/cb","scopes":["openid","email"]}`, idp.srv.URL)

	var pid string
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		p, e := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			Type: sso.TypeOIDC, Name: "Mock-IdP", Enabled: true,
			Config: json.RawMessage(cfg),
		})
		pid = p.ID
		return e
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	var result sso.StartLoginResult
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		r, e := h.repo.StartLogin(ctx, tx, sso.StartLoginRequest{ProviderID: pid})
		result = r
		return e
	}); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if result.AuthURL == "" {
		t.Fatalf("AuthURL should not be empty when OIDC client is injected")
	}
	if !strings.HasPrefix(result.AuthURL, idp.srv.URL+"/authorize?") {
		t.Errorf("AuthURL = %q, want prefix %s/authorize?", result.AuthURL, idp.srv.URL)
	}
	if !strings.Contains(result.AuthURL, "code_challenge=") {
		t.Errorf("AuthURL missing PKCE: %q", result.AuthURL)
	}
	if !strings.Contains(result.AuthURL, "state="+result.State) {
		t.Errorf("AuthURL state mismatch")
	}
}

func TestCompleteLoginOIDCEndToEnd(t *testing.T) {
	// 본 테스트는 miniIdPNonce 글로벌 변수를 변경하므로 t.Parallel() 사용 안 함 (race 회피).
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	idp := newMiniIdP(t, fixedNow(now))
	resolver := &fakeIdentityResolver{uid: testUser}
	h := newOIDCHarness(t, idp, resolver)

	cfg := fmt.Sprintf(`{"issuer":"%s","clientId":"client-XYZ","redirectUri":"https://app/cb","scopes":["openid","email"]}`, idp.srv.URL)

	// 1) provider 생성.
	var pid string
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		p, e := h.repo.CreateProvider(ctx, tx, sso.CreateProviderRequest{
			Type: sso.TypeOIDC, Name: "Mock-IdP", Enabled: true,
			Config: json.RawMessage(cfg),
		})
		pid = p.ID
		return e
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	// 2) StartLogin — attempt.Nonce를 mock IdP의 fixedNonce로 강제 일치시키기 위해
	// 직접 attempt INSERT는 필요 없음 — mock IdP의 fixedNonce 상수와 attempt.Nonce가
	// 일치하지 않으면 ErrNonceMismatch가 나는데, 이를 검증하려면 attempt.Nonce를 mock에 주입해야 함.
	// 본 테스트는 단순화: mock이 attempt.Nonce를 모르므로, 발급 nonce는 별도 토큰을 만들어 직접 검증.

	var result sso.StartLoginResult
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		r, e := h.repo.StartLogin(ctx, tx, sso.StartLoginRequest{ProviderID: pid})
		result = r
		return e
	}); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}

	// mock IdP의 nonce를 attempt.Nonce로 강제 일치 — 동시 실행이 아니라 패턴 검증용.
	miniIdPNonce = result.Attempt.Nonce
	t.Cleanup(func() { miniIdPNonce = "mock-nonce-must-match" })

	// 3) CompleteLogin — IdP code 교환 + id_token 검증 + UpsertExternalIdentity.
	var done sso.CompleteLoginResult
	if err := h.store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		r, e := h.repo.CompleteLogin(ctx, tx, sso.CompleteLoginRequest{
			State: result.State,
			Code:  "auth-code-from-idp",
		})
		done = r
		return e
	}); err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	if done.Identity.ExternalSubject != "user-12345" {
		t.Errorf("ExternalSubject = %q, want user-12345", done.Identity.ExternalSubject)
	}
	if done.Identity.Email != "alice@example.test" {
		t.Errorf("Email = %q", done.Identity.Email)
	}
	if done.Identity.UserID != testUser {
		t.Errorf("UserID = %q, want %q (resolver)", done.Identity.UserID, testUser)
	}
	if !resolver.called {
		t.Errorf("IdentityResolver.ResolveOIDCIdentity not called")
	}

	// audit emit: started + completed(ok=true).
	if h.emitter.loginStarted != 1 || h.emitter.loginCompleted != 1 {
		t.Errorf("audit counts = started:%d completed:%d, want 1/1",
			h.emitter.loginStarted, h.emitter.loginCompleted)
	}
	if len(h.emitter.loginOK) != 1 || !h.emitter.loginOK[0] {
		t.Errorf("loginOK = %v, want [true]", h.emitter.loginOK)
	}
}

// fixedNow는 oidc_test의 fixedClock 헬퍼 격리용 (test 파일 분리).
func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }
