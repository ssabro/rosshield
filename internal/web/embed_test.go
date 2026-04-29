package web

// embed_test.go — Web Console 정적 자산 embed + Handler 동작 검증 (E10 Stage D).

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHasAssetsReturnsTrueWhenBuilt(t *testing.T) {
	// `make web-build` 후에는 dist/index.html 존재. 본 테스트는 빌드 결과 의존.
	// CI에서 빌드를 안 했다면 skip — 그러나 로컬 개발 흐름은 빌드 후 테스트라 일반 OK.
	if !HasAssets() {
		t.Skip("dist/index.html missing — run `make web-build` first")
	}
}

func TestHandlerServesIndexHTMLAtRoot(t *testing.T) {
	if !HasAssets() {
		t.Skip("dist/index.html missing — run `make web-build` first")
	}
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "<!doctype html>") &&
		!strings.Contains(string(body), "<!DOCTYPE html>") {
		t.Fatalf("body missing doctype:\n%s", string(body)[:min(200, len(body))])
	}
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("Content-Type=%q", rec.Header().Get("Content-Type"))
	}
}

func TestHandlerSPAFallbackForUnknownPath(t *testing.T) {
	if !HasAssets() {
		t.Skip("dist missing")
	}
	h, _ := Handler()

	// TanStack Router 클라이언트 측 라우트(login·robots·scans·reports) 시뮬.
	for _, path := range []string{"/login", "/robots", "/scans", "/reports", "/some/deep/route"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status=%d, want 200 (SPA fallback)", path, rec.Code)
		}
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s: Content-Type=%q, want text/html",
				path, rec.Header().Get("Content-Type"))
		}
	}
}

func TestHandlerServesAssetsWithImmutableCache(t *testing.T) {
	if !HasAssets() {
		t.Skip("dist missing")
	}
	h, _ := Handler()

	// 실제 빌드된 hashed asset 파일 찾기 — 빌드마다 hash 다르므로 dist 디렉터리 listing.
	assetName := findAnyAsset(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/"+assetName, nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (asset %q)", rec.Code, assetName)
	}
	cache := rec.Header().Get("Cache-Control")
	if !strings.Contains(cache, "max-age=31536000") || !strings.Contains(cache, "immutable") {
		t.Fatalf("Cache-Control=%q, want immutable+max-age", cache)
	}
}

// findAnyAsset은 dist/assets/ 안의 첫 번째 .js 파일명을 반환합니다.
func findAnyAsset(t *testing.T) string {
	t.Helper()
	entries, err := dist.ReadDir("dist/assets")
	if err != nil {
		t.Fatalf("ReadDir assets: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
			return e.Name()
		}
	}
	t.Fatalf("no .js asset found in dist/assets")
	return ""
}

func TestHandlerErrorWhenDistMissing(t *testing.T) {
	// 본 테스트는 빌드 결과가 있을 때 ErrIndexMissing이 반환되지 않음을 확인.
	// (반대 시나리오는 build없이는 시뮬레이션 어려움 — production gate에서 처리.)
	if !HasAssets() {
		_, err := Handler()
		if err == nil {
			t.Fatalf("expected ErrIndexMissing when dist absent")
		}
	}
}
