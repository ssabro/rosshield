package replication

import (
	"encoding/json"
	"net/http"
	"strings"
)

// standbyExemptPaths는 standby 인스턴스에서도 항상 통과시키는 path prefix 목록입니다.
//
// 포함:
//   - /health, /healthz, /readyz — LB health check
//   - /api/v1/replication/heartbeat — standby 자기 자신이 primary에 보내는 ping
//   - /api/v1/replication/failover — admin이 region B (현재 standby)에서 region A → B
//     failover trigger 가능해야 함 (B가 active로 승격)
//
// 위 외 모든 write method(POST/PUT/PATCH/DELETE)는 409 Conflict로 차단.
var standbyExemptPaths = []string{
	"/health",
	"/healthz",
	"/readyz",
	"/api/v1/replication/heartbeat",
	"/api/v1/replication/failover",
}

// StandbyReadOnlyMiddleware는 standby 인스턴스의 write 요청을 차단합니다.
//
// 동작:
//   - cfg.IsStandby() == false → 모든 요청 통과 (Enabled=false 또는 Role=primary)
//   - method ∈ {GET, HEAD, OPTIONS} → 통과 (read)
//   - path가 standbyExemptPaths 중 하나로 시작 → 통과
//   - 그 외 (POST/PUT/PATCH/DELETE) → 409 Conflict + 응답 body에 primary endpoint 안내
//
// 응답:
//
//	HTTP/1.1 409 Conflict
//	Content-Type: application/json
//	X-Rosshield-Replica-Role: standby
//	X-Rosshield-Primary-Endpoint: <primary endpoint>
//	{"error":"standby_read_only","region":"<self>","primary_endpoint":"<url>"}
//
// 설계: docs/design/notes/multi-region-ha-design.md §4.2 standby read-only + §4.3
// failover (R1 standby read 가능).
func StandbyReadOnlyMiddleware(cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.IsStandby() {
				next.ServeHTTP(w, r)
				return
			}

			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}

			path := r.URL.Path
			for _, prefix := range standbyExemptPaths {
				if path == prefix || strings.HasPrefix(path, prefix+"/") {
					next.ServeHTTP(w, r)
					return
				}
			}

			// write method, exempt 아님 — 409 Conflict.
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Rosshield-Replica-Role", string(cfg.Role))
			if cfg.PrimaryEndpoint != "" {
				w.Header().Set("X-Rosshield-Primary-Endpoint", cfg.PrimaryEndpoint)
			}
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":            "standby_read_only",
				"region":           cfg.Region,
				"primary_endpoint": cfg.PrimaryEndpoint,
				"message":          "instance is replication standby — retry on primary region",
			})
		})
	}
}
