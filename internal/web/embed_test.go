package web

// embed_test.go — Web Console 정적 자산 embed + Handler 동작 검증 (E10 Stage D).

import (
	"encoding/json"
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

// TestEmbedIncludesPWAManifest는 PWA Stage 1 산출물이 Go 바이너리에 임베드되어
// embedded FS로 접근 가능한지 회귀 차단합니다 (design doc §6.1, §9.1).
//
// `web/public/*` 정적 자산은 Vite가 build 시 dist 루트로 그대로 복사 — Go
// `//go:embed dist`가 자동 흡수합니다. 회귀 시나리오: outDir 변경 또는
// vite-plugin-pwa 도입(Stage 2) 시 manifest 파일명이 바뀌어 본 테스트가
// 즉시 알람 역할을 합니다.
func TestEmbedIncludesPWAManifest(t *testing.T) {
	if !HasAssets() {
		t.Skip("dist missing — run `make web-build` first")
	}

	raw, err := dist.ReadFile("dist/manifest.webmanifest")
	if err != nil {
		t.Fatalf("manifest.webmanifest missing in embed: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("manifest.webmanifest empty")
	}

	// 유효 JSON + 필수 키 확인.
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("manifest.webmanifest invalid JSON: %v", err)
	}
	for _, key := range []string{"name", "short_name", "start_url", "display", "theme_color", "icons"} {
		if _, ok := manifest[key]; !ok {
			t.Errorf("manifest.webmanifest missing required key %q", key)
		}
	}
}

// TestEmbedIncludesPWAIcons는 manifest가 참조하는 아이콘 파일들이 함께
// 임베드되어 있는지 확인합니다.
func TestEmbedIncludesPWAIcons(t *testing.T) {
	if !HasAssets() {
		t.Skip("dist missing")
	}

	for _, name := range []string{
		"dist/icon-192.png",
		"dist/icon-512.png",
		"dist/apple-touch-icon.png",
		"dist/favicon.svg",
	} {
		info, err := dist.ReadFile(name)
		if err != nil {
			t.Errorf("missing %s: %v", name, err)
			continue
		}
		if len(info) == 0 {
			t.Errorf("%s empty", name)
		}
	}
}

// TestHandlerServesManifestWithoutCache는 manifest를 Handler가 정상 서빙하는지
// + /assets/* immutable cache 정책 적용 대상이 아님(루트 자산)을 확인합니다.
func TestHandlerServesManifest(t *testing.T) {
	if !HasAssets() {
		t.Skip("dist missing")
	}
	h, _ := Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "rosshield") {
		t.Fatalf("manifest body missing brand name: %s", string(body)[:min(200, len(body))])
	}
	// /assets/*가 아닌 루트 자산 → immutable cache header 없어야 함 (Workbox SW가 차후 관리).
	if cache := rec.Header().Get("Cache-Control"); strings.Contains(cache, "immutable") {
		t.Errorf("manifest unexpectedly immutable: Cache-Control=%q", cache)
	}
}

// TestEmbedIncludesServiceWorker는 PWA Stage 2에서 vite-plugin-pwa generateSW가
// 생성하는 sw.js가 Go 바이너리에 임베드되어 있는지 회귀 차단합니다 (design doc
// §6.2, §9.1). 회귀 시나리오: vite.config.ts에서 VitePWA plugin 누락 또는
// outDir 변경으로 sw.js가 dist에서 사라지는 경우 즉시 알람.
func TestEmbedIncludesServiceWorker(t *testing.T) {
	if !HasAssets() {
		t.Skip("dist missing — run `make web-build` first")
	}

	raw, err := dist.ReadFile("dist/sw.js")
	if err != nil {
		t.Fatalf("sw.js missing in embed: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("sw.js empty")
	}

	// vite-plugin-pwa 1.0+ 의 generateSW는 AMD `define([...])` 패턴으로 workbox
	// 청크 로드 + precacheAndRoute + NavigationRoute(denylist:/api) 호출.
	sw := string(raw)
	if !strings.Contains(sw, "precacheAndRoute") {
		t.Errorf("sw.js missing precacheAndRoute (Workbox generateSW pattern broken)")
	}
	if !strings.Contains(sw, "NavigationRoute") {
		t.Errorf("sw.js missing NavigationRoute (SPA fallback config 누락)")
	}
	// /api/* 우회(D-PWA-7) 회귀 차단 — 본 정책이 사라지면 SW가 API 응답 캐싱 시작
	// 위험. 정규식 이스케이프 형태(`\/api\/`)로 sw.js에 직렬화됨.
	if !strings.Contains(sw, `\/api\/`) {
		t.Errorf("sw.js missing /api/ denylist (D-PWA-7 멀티테넌시 가드 누락)")
	}
}

// TestEmbedIncludesWorkboxRuntime은 Workbox 런타임 청크(workbox-*.js)가 dist에
// 함께 임베드되어 있는지 확인합니다.
func TestEmbedIncludesWorkboxRuntime(t *testing.T) {
	if !HasAssets() {
		t.Skip("dist missing")
	}

	entries, err := dist.ReadDir("dist")
	if err != nil {
		t.Fatalf("ReadDir dist: %v", err)
	}
	var workboxFound bool
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// workbox-{hash}.js 패턴.
		if strings.HasPrefix(name, "workbox-") && strings.HasSuffix(name, ".js") {
			workboxFound = true
			info, err := dist.ReadFile("dist/" + name)
			if err != nil {
				t.Errorf("workbox chunk read fail %s: %v", name, err)
				continue
			}
			if len(info) == 0 {
				t.Errorf("workbox chunk %s empty", name)
			}
		}
	}
	if !workboxFound {
		t.Errorf("no workbox-*.js chunk found in dist (vite-plugin-pwa generateSW 미작동?)")
	}
}
