// oidc.go — OIDC 표준 discovery + Authorization Code + PKCE + id_token 검증 (E20-B).
//
// 구현 정책 (의도적 표준 라이브러리만):
//
//	P3(에어갭 1급) + dep 최소화 + 옵션화(P10)를 위해 외부 OIDC 라이브러리 도입 없이
//	net/http + crypto/* + encoding/json + golang-jwt(이미 dep)만 사용합니다.
//	검증 비용: 본 파일은 단일 IdP 흐름(OIDC Core 1.0 Authorization Code + PKCE)만 다루고
//	Implicit·Hybrid·Device·Dynamic Registration 등 비대상 흐름은 의도적 미구현.
//
// 참조 RFC/스펙:
//
//	OIDC Core 1.0          §3.1 (Authorization Code Flow)
//	RFC 8414               OAuth 2.0 Authorization Server Metadata (.well-known/openid-configuration)
//	RFC 7636               PKCE (S256)
//	RFC 7517·7518          JWK / JWA (RS256·ES256)
//
// 검증 단계 (VerifyIDToken):
//
//  1. JWT 분해 → header.kid 추출.
//  2. JWKS endpoint fetch (캐시 X — 단순화. 후속 stage에서 TTL 캐시 도입 가능).
//  3. kid 매칭 키로 서명 검증 (RS256 또는 ES256).
//  4. iss == issuer, aud contains clientID, exp > now+leeway, nonce == expectedNonce 확인.
//  5. sub·email·email_verified 등 응답용 claims 추출.
package sso

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// OIDCConfig는 Provider.Config의 OIDC 형식을 디코딩한 struct입니다.
//
// JSON 예:
//
//	{"issuer":"https://accounts.google.com","clientId":"...","clientSecret":"...",
//	 "redirectUri":"https://app/callback","scopes":["openid","email","profile"]}
//
// scopes 빈 배열 → 기본값 ["openid","email","profile"] 사용.
type OIDCConfig struct {
	Issuer       string   `json:"issuer"`
	ClientID     string   `json:"clientId"`
	ClientSecret string   `json:"clientSecret"`
	RedirectURI  string   `json:"redirectUri"`
	Scopes       []string `json:"scopes"`
}

// ParseOIDCConfig는 Provider.Config(json.RawMessage)를 OIDCConfig로 디코딩합니다.
//
// 검증:
//   - issuer·clientId·redirectUri 비어 있으면 ErrInvalidOIDCConfig.
//   - clientSecret은 confidential client만 필수 — public client(PKCE-only)도 허용되므로 본 검증은 Exchange 단계로 위임.
func ParseOIDCConfig(raw json.RawMessage) (OIDCConfig, error) {
	var c OIDCConfig
	if len(raw) == 0 {
		return c, ErrInvalidOIDCConfig
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("sso: parse oidc config: %w", err)
	}
	c.Issuer = strings.TrimSpace(c.Issuer)
	c.ClientID = strings.TrimSpace(c.ClientID)
	c.RedirectURI = strings.TrimSpace(c.RedirectURI)
	if c.Issuer == "" || c.ClientID == "" || c.RedirectURI == "" {
		return c, ErrInvalidOIDCConfig
	}
	if len(c.Scopes) == 0 {
		c.Scopes = []string{"openid", "email", "profile"}
	}
	return c, nil
}

// IDTokenClaims는 검증된 id_token에서 추출된 표준 + 선택 claims입니다.
//
// 호출자(application)는 Subject·Email을 ExternalIdentity 매핑에 사용.
// EmailVerified=false면 호출자가 email 매칭을 거부할지 결정.
type IDTokenClaims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Issuer        string
	Audience      []string
	IssuedAt      time.Time
	ExpiresAt     time.Time
	Nonce         string
}

// OIDCDiscovery는 RFC 8414 .well-known/openid-configuration 응답의 부분집합입니다.
//
// 본 stage가 사용하는 4개 필드만 정의 — 나머지(end_session_endpoint·revocation_endpoint 등)는 후속 stage에서.
type OIDCDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// OIDCClient는 IdP HTTP 호출 + 검증 책임을 갖는 stateless helper입니다.
//
// 의도적으로 application service에 직접 노출 — sso.Service interface에 OIDC-only 메서드를
// 추가하면 SAML과 표면이 비대해지므로, sqliterepo가 본 client를 호출하는 어댑터 패턴.
//
// HTTPClient는 nil 가능 — 그 경우 http.DefaultClient 사용. 테스트는 mock IdP에 맞춰
// 별 client 주입(또는 timeout 단축).
type OIDCClient struct {
	HTTPClient *http.Client

	// Now는 시각 주입 — 테스트에서 결정론적 검증.
	Now func() time.Time

	// Leeway는 exp/iat 검증 시 시계 동기 오차 허용 (기본 60s).
	Leeway time.Duration
}

// NewOIDCClient는 default 설정 client를 반환합니다.
func NewOIDCClient() *OIDCClient {
	return &OIDCClient{
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		Now:        time.Now,
		Leeway:     60 * time.Second,
	}
}

// httpClient는 nil-safe accessor입니다.
func (c *OIDCClient) httpClient() *http.Client {
	if c == nil || c.HTTPClient == nil {
		return http.DefaultClient
	}
	return c.HTTPClient
}

func (c *OIDCClient) now() time.Time {
	if c == nil || c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

func (c *OIDCClient) leeway() time.Duration {
	if c == nil || c.Leeway <= 0 {
		return 60 * time.Second
	}
	return c.Leeway
}

// Discover는 issuer/.well-known/openid-configuration을 GET하여 metadata를 반환합니다.
//
// RFC 8414 §3: issuer는 trailing slash 없는 URL. discovery URL은 issuer + "/.well-known/openid-configuration".
// 응답 issuer는 요청 issuer와 정확히 일치해야 함(RFC 8414 §3.3 — issuer mismatch 방어).
func (c *OIDCClient) Discover(ctx context.Context, issuer string) (OIDCDiscovery, error) {
	if strings.TrimSpace(issuer) == "" {
		return OIDCDiscovery{}, ErrInvalidOIDCConfig
	}
	u := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return OIDCDiscovery{}, fmt.Errorf("sso: discover request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return OIDCDiscovery{}, fmt.Errorf("%w: discover: %v", ErrIdPHTTP, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return OIDCDiscovery{}, fmt.Errorf("%w: discover status %d", ErrIdPHTTP, resp.StatusCode)
	}
	var d OIDCDiscovery
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&d); err != nil {
		return OIDCDiscovery{}, fmt.Errorf("%w: discover decode: %v", ErrIdPHTTP, err)
	}
	if d.Issuer == "" || d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.JWKSURI == "" {
		return OIDCDiscovery{}, fmt.Errorf("%w: discover missing fields", ErrIdPHTTP)
	}
	if strings.TrimRight(d.Issuer, "/") != strings.TrimRight(issuer, "/") {
		return OIDCDiscovery{}, fmt.Errorf("%w: issuer mismatch (want %q, got %q)", ErrIdPHTTP, issuer, d.Issuer)
	}
	return d, nil
}

// BuildAuthURL은 OIDC authorization endpoint URL을 빌드합니다.
//
// Authorization Code + PKCE(S256) 흐름:
//
//	GET <authorization_endpoint>?
//	    response_type=code &
//	    client_id=<clientID> &
//	    redirect_uri=<redirectURI> &
//	    scope=<space-joined scopes> &
//	    state=<csrf token> &
//	    code_challenge=<S256(verifier)> &
//	    code_challenge_method=S256 &
//	    nonce=<id_token nonce>
func (c *OIDCClient) BuildAuthURL(ctx context.Context, cfg OIDCConfig, state, pkceVerifier, nonce string) (string, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(pkceVerifier) == "" || strings.TrimSpace(nonce) == "" {
		return "", ErrInvalidOIDCArgs
	}
	d, err := c.Discover(ctx, cfg.Issuer)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(d.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("%w: parse authorization_endpoint: %v", ErrIdPHTTP, err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", cfg.RedirectURI)
	q.Set("scope", strings.Join(cfg.Scopes, " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", pkceChallenge(pkceVerifier))
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// pkceChallenge는 RFC 7636 §4.2 S256 변환을 수행합니다 (BASE64URL(SHA256(verifier))).
func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// tokenResponse는 token_endpoint POST 응답입니다.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	IDToken     string `json:"id_token"`
	Scope       string `json:"scope"`
}

// ExchangeCode는 token_endpoint에 authorization_code grant POST 후 (idToken, accessToken)을 반환합니다.
//
// confidential client는 client_secret을 함께 전송, public client는 PKCE만으로 충분.
// 본 구현은 둘 다 지원 — clientSecret이 비어 있으면 PKCE-only.
func (c *OIDCClient) ExchangeCode(ctx context.Context, cfg OIDCConfig, code, pkceVerifier string) (idToken, accessToken string, err error) {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(pkceVerifier) == "" {
		return "", "", ErrInvalidOIDCArgs
	}
	d, derr := c.Discover(ctx, cfg.Issuer)
	if derr != nil {
		return "", "", derr
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", cfg.RedirectURI)
	form.Set("client_id", cfg.ClientID)
	form.Set("code_verifier", pkceVerifier)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", fmt.Errorf("sso: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", "", fmt.Errorf("%w: token: %v", ErrIdPHTTP, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("%w: token status %d body=%s", ErrIdPHTTP, resp.StatusCode, snippet(body))
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", "", fmt.Errorf("%w: token decode: %v", ErrIdPHTTP, err)
	}
	if tr.IDToken == "" {
		return "", "", fmt.Errorf("%w: token response missing id_token", ErrIdPHTTP)
	}
	return tr.IDToken, tr.AccessToken, nil
}

// snippet는 에러 메시지에 안전하게 포함시킬 수 있도록 응답 body를 자릅니다.
func snippet(b []byte) string {
	const max = 200
	s := string(b)
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// jwk는 JWKS 응답의 단일 키입니다 (필요한 필드만).
type jwk struct {
	Kty string `json:"kty"` // "RSA" | "EC"
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`

	// RSA fields
	N string `json:"n"`
	E string `json:"e"`

	// EC fields
	Crv string `json:"crv"` // "P-256" | "P-384" | "P-521"
	X   string `json:"x"`
	Y   string `json:"y"`
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// fetchJWKS는 jwks_uri에서 키 셋을 GET합니다 (TTL 캐시 없음 — 후속 stage에서 추가 가능).
func (c *OIDCClient) fetchJWKS(ctx context.Context, jwksURI string) (jwkSet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return jwkSet{}, fmt.Errorf("sso: jwks request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return jwkSet{}, fmt.Errorf("%w: jwks: %v", ErrIdPHTTP, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return jwkSet{}, fmt.Errorf("%w: jwks status %d", ErrIdPHTTP, resp.StatusCode)
	}
	var ks jwkSet
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ks); err != nil {
		return jwkSet{}, fmt.Errorf("%w: jwks decode: %v", ErrIdPHTTP, err)
	}
	return ks, nil
}

// VerifyIDToken은 id_token JWT를 JWKS로 서명 검증한 후 표준 claims를 추출합니다.
//
// 검증 항목:
//
//	signature — RS256(RSA) 또는 ES256(EC P-256). 그 외 알고리즘은 ErrUnsupportedAlg.
//	iss       — cfg.Issuer와 정확히 일치 (trailing slash 무시).
//	aud       — cfg.ClientID 포함.
//	exp       — now - leeway < exp.
//	nonce     — expectedNonce와 일치.
func (c *OIDCClient) VerifyIDToken(ctx context.Context, cfg OIDCConfig, idToken, expectedNonce string) (IDTokenClaims, error) {
	if strings.TrimSpace(idToken) == "" {
		return IDTokenClaims{}, ErrInvalidOIDCArgs
	}
	d, err := c.Discover(ctx, cfg.Issuer)
	if err != nil {
		return IDTokenClaims{}, err
	}
	ks, err := c.fetchJWKS(ctx, d.JWKSURI)
	if err != nil {
		return IDTokenClaims{}, err
	}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256", "ES256"}),
		jwt.WithLeeway(c.leeway()),
		jwt.WithIssuer(strings.TrimRight(cfg.Issuer, "/")),
		jwt.WithAudience(cfg.ClientID),
		jwt.WithTimeFunc(c.now),
	)
	tok, err := parser.Parse(idToken, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		key, kerr := selectKey(ks, kid, t.Method.Alg())
		if kerr != nil {
			return nil, kerr
		}
		return key, nil
	})
	if err != nil {
		return IDTokenClaims{}, fmt.Errorf("%w: %v", ErrIDTokenInvalid, err)
	}
	if !tok.Valid {
		return IDTokenClaims{}, ErrIDTokenInvalid
	}

	mc, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return IDTokenClaims{}, ErrIDTokenInvalid
	}

	out, err := claimsFromMap(mc)
	if err != nil {
		return IDTokenClaims{}, err
	}
	if expectedNonce != "" && out.Nonce != expectedNonce {
		return IDTokenClaims{}, ErrNonceMismatch
	}
	if out.Subject == "" {
		return IDTokenClaims{}, fmt.Errorf("%w: missing sub", ErrIDTokenInvalid)
	}
	return out, nil
}

// selectKey는 JWKS에서 kid·alg에 맞는 공개키를 반환합니다.
//
// kid 없으면 단일 키 셋만 매칭(권장 X — IdP가 kid 없이 키를 회전하면 충돌 가능).
func selectKey(ks jwkSet, kid, alg string) (any, error) {
	for _, k := range ks.Keys {
		if kid != "" && k.Kid != kid {
			continue
		}
		switch alg {
		case "RS256":
			if k.Kty != "RSA" {
				continue
			}
			return rsaKeyFromJWK(k)
		case "ES256":
			if k.Kty != "EC" {
				continue
			}
			return ecKeyFromJWK(k)
		default:
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedAlg, alg)
		}
	}
	return nil, fmt.Errorf("%w: kid=%s alg=%s", ErrJWKNotFound, kid, alg)
}

// rsaKeyFromJWK는 RFC 7518 §6.3 RSA JWK → *rsa.PublicKey 변환입니다.
func rsaKeyFromJWK(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("%w: rsa.n: %v", ErrJWKNotFound, err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("%w: rsa.e: %v", ErrJWKNotFound, err)
	}
	// e는 보통 작은 정수 (65537). big-endian으로 padding.
	var eBuf [8]byte
	if len(eBytes) > 8 {
		return nil, fmt.Errorf("%w: rsa.e too long", ErrJWKNotFound)
	}
	copy(eBuf[8-len(eBytes):], eBytes)
	e := binary.BigEndian.Uint64(eBuf[:])
	if e == 0 || e > (1<<31-1) {
		return nil, fmt.Errorf("%w: rsa.e out of range", ErrJWKNotFound)
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(e),
	}, nil
}

// ecKeyFromJWK는 RFC 7518 §6.2 EC JWK → *ecdsa.PublicKey 변환입니다 (P-256/P-384/P-521).
func ecKeyFromJWK(k jwk) (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("%w: ec.crv=%s", ErrUnsupportedAlg, k.Crv)
	}
	x, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("%w: ec.x: %v", ErrJWKNotFound, err)
	}
	y, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, fmt.Errorf("%w: ec.y: %v", ErrJWKNotFound, err)
	}
	return &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(x),
		Y:     new(big.Int).SetBytes(y),
	}, nil
}

// claimsFromMap은 jwt.MapClaims에서 IDTokenClaims를 추출합니다.
//
// audience는 string 또는 []string 모두 허용 (RFC 7519 §4.1.3).
func claimsFromMap(mc jwt.MapClaims) (IDTokenClaims, error) {
	var out IDTokenClaims
	if v, ok := mc["sub"].(string); ok {
		out.Subject = v
	}
	if v, ok := mc["email"].(string); ok {
		out.Email = v
	}
	if v, ok := mc["email_verified"].(bool); ok {
		out.EmailVerified = v
	}
	if v, ok := mc["name"].(string); ok {
		out.Name = v
	}
	if v, ok := mc["iss"].(string); ok {
		out.Issuer = v
	}
	if v, ok := mc["nonce"].(string); ok {
		out.Nonce = v
	}
	switch a := mc["aud"].(type) {
	case string:
		out.Audience = []string{a}
	case []any:
		for _, x := range a {
			if s, ok := x.(string); ok {
				out.Audience = append(out.Audience, s)
			}
		}
	}
	if v, ok := mc["iat"].(float64); ok {
		out.IssuedAt = time.Unix(int64(v), 0).UTC()
	}
	if v, ok := mc["exp"].(float64); ok {
		out.ExpiresAt = time.Unix(int64(v), 0).UTC()
	}
	return out, nil
}

// === sentinels (sso 패키지 추가) ===

var (
	// ErrInvalidOIDCConfig는 Provider.Config가 OIDC 형식 검증에 실패했음을 의미합니다.
	ErrInvalidOIDCConfig = errors.New("sso: oidc config invalid (issuer/clientId/redirectUri required)")
	// ErrInvalidOIDCArgs는 BuildAuthURL/Exchange/Verify에 빈 인자가 들어왔음을 의미합니다.
	ErrInvalidOIDCArgs = errors.New("sso: oidc arguments are missing (state/code/verifier/nonce required)")
	// ErrIdPHTTP는 IdP HTTP 호출 실패(network·non-2xx·malformed body)를 묶습니다 — handler에서 502 매핑.
	ErrIdPHTTP = errors.New("sso: idp http error")
	// ErrIDTokenInvalid는 id_token JWT 검증 실패 (서명·iss·aud·exp 등)를 의미합니다.
	ErrIDTokenInvalid = errors.New("sso: id_token verification failed")
	// ErrNonceMismatch는 id_token nonce가 영속된 attempt nonce와 다름.
	ErrNonceMismatch = errors.New("sso: id_token nonce mismatch")
	// ErrUnsupportedAlg는 RS256/ES256 외 서명 알고리즘이 사용됨.
	ErrUnsupportedAlg = errors.New("sso: id_token signature algorithm not supported (only RS256/ES256)")
	// ErrJWKNotFound는 JWKS에 매칭되는 kid·alg 키가 없음.
	ErrJWKNotFound = errors.New("sso: matching JWK not found")
)
