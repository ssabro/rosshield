// Package web는 Web Console 빌드 산출물(`web/dist/`)을 Go 바이너리에 임베드합니다 (E10 Stage D, R12-11).
//
// 단일 바이너리 원칙(P7) — `rosshield-server` 한 파일이 백엔드 + Web Console 정적 자산을
// 모두 서빙. 에어갭 1급(P3) 정합 — 외부 CDN·NPM 런타임 의존 0.
//
// 빌드 흐름:
//
//	cd web && pnpm build   → ../internal/web/dist/{index.html, assets/*}
//	go build ./cmd/...      → //go:embed가 dist 트리를 바이너리에 흡수
//
// 정적 서빙은 `Handler()`가 노출하는 http.Handler — fall-through SPA 라우팅 지원
// (TanStack Router file-based 경로는 클라이언트 측 — 모든 unmatched path는 index.html
// 로 fallback).
package web

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"strings"
)

// dist는 web/dist 빌드 산출물 트리입니다.
//
// `web/`이 아니라 `internal/web/dist`로 outDir 지정한 이유: Go `//go:embed`는 패키지
// 디렉터리 기준 상대 경로만 허용 — `..` 사용 불가. vite.config.ts가 빌드 결과를 본
// 패키지로 직접 출력해 embed 호환.
//
//go:embed dist
var dist embed.FS

// IndexPath는 SPA fallback 대상 파일입니다.
const IndexPath = "dist/index.html"

// AssetsPrefix는 Vite가 출력하는 자산 디렉터리 prefix입니다.
const AssetsPrefix = "/assets/"

// ErrIndexMissing은 dist/index.html이 빌드 안 된 상태를 표현합니다 (개발 환경 가드).
var ErrIndexMissing = errors.New("web: dist/index.html missing — run `make web-build` first")

// Handler는 Web Console 정적 자산을 서빙하는 http.Handler를 반환합니다.
//
// 라우팅 규칙:
//   - `/assets/*` — Vite hashed 자산 (cache-friendly Long-TTL, immutable)
//   - 그 외 (`/`, `/login`, `/robots`, etc.) — index.html (TanStack Router가 클라이언트 측 처리)
//
// 빌드 결과가 없으면(개발 모드에서 `make web-build` 미실행) ErrIndexMissing 반환 —
// caller(bootstrap)가 graceful 503 또는 에러 메시지로 변환.
func Handler() (http.Handler, error) {
	if _, err := dist.ReadFile(IndexPath); err != nil {
		return nil, ErrIndexMissing
	}
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, err
	}
	indexBytes, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 정적 자산은 GET/HEAD만 허용 — POST/PUT/DELETE 등은 405로 거부해 다른 mux 핸들러
		// (`POST /healthz` 같은 method-specific 라우트)와 의미론적 충돌 회피.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := r.URL.Path

		// /assets/*는 immutable hashed bundle — 1년 cache, FileServer 직접 위임.
		if strings.HasPrefix(path, AssetsPrefix) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fileServer.ServeHTTP(w, r)
			return
		}

		// 기타 자산(favicon 등) 우선 시도 — 없으면 index fallback.
		if path != "/" && path != "" {
			cleanPath := strings.TrimPrefix(path, "/")
			if _, err := fs.Stat(sub, cleanPath); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// SPA fallback — TanStack Router 클라이언트 측 처리.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(indexBytes)
	}), nil
}

// HasAssets는 dist/index.html 존재 여부를 빠르게 확인합니다 (bootstrap 진단용).
func HasAssets() bool {
	_, err := dist.ReadFile(IndexPath)
	return err == nil
}
