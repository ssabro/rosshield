package handlers_test

// auth_cookie_test.go — C6 HttpOnly cookie 모드 통합 테스트.
//
// 시나리오:
//  1. Login with `X-Cookie-Auth: true` → 본문 refreshToken 빈 값, Set-Cookie 부착
//  2. Refresh from cookie → 200 + 새 access + 새 cookie
//  3. Refresh from body (legacy) → 200 + 새 refresh가 본문에
//  4. Logout with cookie → 204 + 빈 Set-Cookie (maxAge=0/-1)
//  5. Refresh after logout → 401
//
// 패턴: handlers_test.go의 newFixture를 재사용 — fixture는 admin user를 시드해 둔 상태.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const cookieAuthHeader = "X-Cookie-Auth"
const refreshCookieName = "rosshield_refresh"

// loginCookieMode은 X-Cookie-Auth: true 헤더로 login → access + cookie 반환.
func loginCookieMode(t *testing.T, f *testFixture) (accessToken string, refreshCookie *http.Cookie) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": f.email, "password": f.password})
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(cookieAuthHeader, "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.AccessToken == "" {
		t.Fatalf("empty accessToken")
	}
	if out.RefreshToken != "" {
		t.Fatalf("cookie 모드에서 본문에 refreshToken이 노출됨: %q", out.RefreshToken)
	}
	for _, c := range resp.Cookies() {
		if c.Name == refreshCookieName {
			refreshCookie = c
			break
		}
	}
	if refreshCookie == nil || refreshCookie.Value == "" {
		t.Fatalf("Set-Cookie %q 누락", refreshCookieName)
	}
	return out.AccessToken, refreshCookie
}

func TestLoginCookieMode_OmitsRefreshFromBodyAndSetsCookie(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	_, cookie := loginCookieMode(t, f)
	if !cookie.HttpOnly {
		t.Errorf("HttpOnly=false, want true")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite=%v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/api/v1/auth" {
		t.Errorf("Path=%q, want /api/v1/auth", cookie.Path)
	}
	if cookie.MaxAge <= 0 {
		t.Errorf("MaxAge=%d, want > 0", cookie.MaxAge)
	}
}

func TestRefreshFromCookie_RotatesAndSetsNewCookie(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	_, cookie := loginCookieMode(t, f)

	// cookie를 동봉해 refresh 요청 (본문 비움).
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/auth/refresh", nil)
	req.Header.Set(cookieAuthHeader, "true")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("refresh status=%d body=%s", resp.StatusCode, string(raw))
	}

	var out struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.AccessToken == "" {
		t.Errorf("empty accessToken after refresh")
	}
	if out.RefreshToken != "" {
		t.Errorf("cookie 모드에서 본문에 refreshToken 노출: %q", out.RefreshToken)
	}

	var newCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == refreshCookieName {
			newCookie = c
			break
		}
	}
	if newCookie == nil || newCookie.Value == "" {
		t.Fatalf("새 Set-Cookie 부재")
	}
	if newCookie.Value == cookie.Value {
		t.Errorf("rotation 안 됨 — 새 refresh 값이 이전과 동일")
	}
}

func TestRefreshFromBody_LegacyModeReturnsRefreshInBody(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	// legacy login (헤더 없음) → 본문에 refresh.
	body, _ := json.Marshal(map[string]string{"email": f.email, "password": f.password})
	resp, err := http.Post(f.server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	var login struct {
		RefreshToken string `json:"refreshToken"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&login)
	_ = resp.Body.Close()
	if login.RefreshToken == "" {
		t.Fatalf("legacy login에서 본문 refreshToken 비어 있음")
	}

	// 본문에 refresh 담아 refresh 요청 (cookie 헤더 없음).
	rb, _ := json.Marshal(map[string]string{"refreshToken": login.RefreshToken})
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/auth/refresh", bytes.NewReader(rb))
	req.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp2.Body)
		t.Fatalf("refresh status=%d body=%s", resp2.StatusCode, string(raw))
	}
	var out struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&out)
	if out.AccessToken == "" || out.RefreshToken == "" {
		t.Errorf("legacy refresh: tokens 비어 있음 — access=%q refresh=%q",
			out.AccessToken, out.RefreshToken)
	}
}

func TestLogoutClearsCookieAndRevokesRefresh(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	_, cookie := loginCookieMode(t, f)

	// logout — cookie 동봉.
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/auth/logout", nil)
	req.Header.Set(cookieAuthHeader, "true")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("logout status=%d body=%s", resp.StatusCode, string(raw))
	}

	var clear *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == refreshCookieName {
			clear = c
			break
		}
	}
	if clear == nil {
		t.Fatalf("logout 응답에 cookie clear 헤더 없음")
	}
	if clear.MaxAge >= 0 && clear.Value != "" {
		t.Errorf("cookie clear 안 됨 — MaxAge=%d Value=%q", clear.MaxAge, clear.Value)
	}

	// 같은 refresh로 다시 refresh 시도 → 401 (revoke 됨).
	req2, _ := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/auth/refresh", nil)
	req2.Header.Set(cookieAuthHeader, "true")
	req2.AddCookie(cookie)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("post-logout refresh: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusUnauthorized {
		raw, _ := io.ReadAll(resp2.Body)
		t.Fatalf("status=%d, want 401 (refresh revoked); body=%s", resp2.StatusCode, string(raw))
	}
}

func TestRefreshWithoutTokenReturns401(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	resp, err := http.Post(f.server.URL+"/api/v1/auth/refresh", "application/json",
		strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}
