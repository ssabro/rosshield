package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/ssabro/rosshield/internal/domain/audit"
)

// RequireLeaderForWrites는 E25 Stage 3 미들웨어입니다.
//
// 동작:
//   - rp == nil → HA 비활성, 모든 요청 통과 (legacy 단일 인스턴스)
//   - method ∈ {GET, HEAD, OPTIONS} → read 요청, follower도 통과
//   - method ∈ {POST, PUT, PATCH, DELETE}:
//   - leader → 통과
//   - follower → 503 Service Unavailable + Retry-After: 5 + body NOT_LEADER
//
// LB(nginx upstream proxy_next_upstream http_503)가 follower 응답을 보고 다음
// upstream(leader)으로 자동 retry. 클라이언트는 투명하게 leader로 라우팅됨.
//
// 도메인 레벨 audit append leader-gate(Stage 2)가 이미 chain 손상을 차단하지만,
// 본 미들웨어가 사용자 친화적인 503 응답으로 LB 통합을 단순화합니다.
//
// 설계: docs/design/notes/e25-ha-design.md §5.2 follower의 write 요청 처리.
func RequireLeaderForWrites(rp audit.RoleProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rp == nil {
				next.ServeHTTP(w, r)
				return
			}
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if rp.IsLeader() {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": false,
				"error": map[string]string{
					"code":    "NOT_LEADER",
					"message": "instance is follower — retry on leader",
				},
			})
		})
	}
}
