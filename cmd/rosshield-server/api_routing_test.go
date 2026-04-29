package main

// api_routing_test.go — Stage D Exit 검증: bootstrap.newMux가 handlers.Mount된 API
// 라우트(/api/v1/*)를 정확히 노출하는지 확인. handlers 단위는 internal/api/handlers
// 가 검증, 본 테스트는 결선(bootstrap → newMux → handlers)만 검증.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewMuxExposesAPIRoutes(t *testing.T) {
	t.Parallel()
	p := newTestPlatform(t)
	mux := newMux(p)

	// auth/login: POST endpoint — bare GET이라 405 또는 4xx (라우트 자체는 살아있음).
	cases := []struct {
		method string
		path   string
		want   []int // 허용 가능한 status (404 X — 그게 라우팅 누락 신호)
	}{
		{"POST", "/api/v1/auth/login", []int{200, 400, 401, 405}}, // 빈 body면 400/401
		{"GET", "/api/v1/auth/me", []int{401}},                    // 토큰 없음 → 401
		{"GET", "/api/v1/robots", []int{401}},
		{"POST", "/api/v1/scans", []int{401}},
		{"GET", "/api/v1/reports", []int{401}},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		ok := false
		for _, w := range c.want {
			if rec.Code == w {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("%s %s → status=%d, want one of %v (404는 라우팅 누락)",
				c.method, c.path, rec.Code, c.want)
		}
	}
}

func TestHealthzPreservedAfterAPIMount(t *testing.T) {
	t.Parallel()
	p := newTestPlatform(t)
	mux := newMux(p)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz status=%d, want 200", rec.Code)
	}
}
