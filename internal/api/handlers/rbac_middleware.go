package handlers

// rbac_middleware.go — RBAC role check middleware (RBAC Stage 1).
//
// AuthMiddleware가 ctx에 주입한 AccessClaims.Roles를 검사해 요구 role 중 하나라도
// 일치하면 통과, 아니면 403 Forbidden.
//
// 사용 예 (handlers.go Mount):
//
//	adminGroup := r.Group(...)
//	adminGroup.Use(h.AuthMiddleware)
//	adminGroup.Use(h.RequireRole("admin"))
//	adminGroup.Post("/api/v1/sso/providers", h.CreateSSOProvider)
//
// 호출 순서: AuthMiddleware 다음에 RequireRole. AuthMiddleware가 미적용이면
// claims가 비어 있어 403 (의도 — protected route만 RBAC 적용).
//
// admin 와일드카드: tenant.Role.HasPermission("*") 패턴과 일관 — admin 역할 보유 시
// 모든 admin gate 통과. 다른 role(auditor·operator·custom)은 명시적 RequireRole에 포함될 때만.

import (
	"net/http"
)

// RequireRole은 protected handler를 감싸는 middleware factory입니다.
//
// 동작:
//   - claims.Roles와 allowed 중 하나 이상 일치 → next.ServeHTTP
//   - 일치 0 → 403 Forbidden + JSON {"error": "..."}
//   - claims 부재 → 401 (AuthMiddleware 미적용 신호 — 의도하지 않은 mount 방어)
//
// allowed가 비어 있으면 항상 401 처리(misconfiguration 방어 — Mount에서 RequireRole()로 호출 금지).
func (h *Handlers) RequireRole(allowed ...string) func(http.Handler) http.Handler {
	allowSet := make(map[string]struct{}, len(allowed))
	for _, r := range allowed {
		if r != "" {
			allowSet[r] = struct{}{}
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(allowSet) == 0 {
				// misconfigured RequireRole() 호출 — fail closed.
				writeError(w, http.StatusInternalServerError, "RequireRole misconfigured: no allowed roles")
				return
			}
			claims, ok := claimsFromContext(r.Context())
			if !ok || claims.Subject == "" {
				// AuthMiddleware 미적용 또는 토큰 비정상.
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			for _, role := range claims.Roles {
				if _, allowed := allowSet[role]; allowed {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeError(w, http.StatusForbidden, "insufficient role")
		})
	}
}
