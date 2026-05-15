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
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/authz"
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

// RequirePermission은 세분 RBAC Stage 3 — resource × action 단위 인가 미들웨어 factory입니다.
//
// design doc §7 Stage 3 산출. 동작 절차:
//
//  1. AuthMiddleware가 ctx에 주입한 AccessClaims 추출 — 부재 시 401.
//  2. claims.Bindings → authz.Subject 변환. Bindings가 비어 있으면 D-RBAC-7 호환 정책에
//     따라 claims.Roles를 모두 tenant scope binding으로 fallback 변환 (옛 토큰 호환).
//  3. URL에서 fleetID param을 추출 (chi.URLParam) — 있으면 Subject.FleetID에 주입,
//     없으면 빈 문자열(tenant 글로벌 요청).
//  4. authz.Decide(sub, resource, action) 호출.
//  5. Allow → next.ServeHTTP / Deny → 403 + {"error":"forbidden","reason":<Decision.Reason>}.
//
// 본 factory는 RequireRole 와 호환 — 기존 admin gate를 점진 교체합니다 (Stage 4).
func (h *Handlers) RequirePermission(resource authz.Resource, action authz.Action) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := claimsFromContext(r.Context())
			if !ok || claims.Subject == "" {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}

			sub := authz.Subject{
				Bindings: bindingsForSubject(claims),
				FleetID:  chi.URLParam(r, "fleetID"),
			}

			d := authz.Decide(sub, resource, action)
			if d.Allow {
				next.ServeHTTP(w, r)
				return
			}

			// 403 응답 — Decision.Reason을 함께 반환해 디버깅·감사 로그 친화.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":  "forbidden",
				"reason": d.Reason,
			})
		})
	}
}

// bindingsForSubject는 claims에서 authz용 RoleBinding 슬라이스를 빌드합니다.
//
// D-RBAC-7 호환 정책:
//   - claims.Bindings 비어 있지 않음 → 그대로 사용 (Stage 3+ 토큰).
//   - 비어 있음 → claims.Roles 를 모두 tenant scope binding으로 fallback (옛 토큰).
//
// 본 변환은 도메인 → PDP 변환 — DDD 경계 §5에 따라 호출자(middleware)에서 수행합니다.
func bindingsForSubject(claims tenant.AccessClaims) []authz.RoleBinding {
	if len(claims.Bindings) > 0 {
		out := make([]authz.RoleBinding, len(claims.Bindings))
		for i, b := range claims.Bindings {
			out[i] = authz.RoleBinding{
				RoleName:  b.Role,
				ScopeType: authz.ScopeType(b.ScopeType),
				ScopeID:   b.ScopeID,
			}
		}
		return out
	}
	// Legacy fallback — Bindings 부재 시 Roles를 모두 tenant scope로 가정.
	if len(claims.Roles) == 0 {
		return nil
	}
	out := make([]authz.RoleBinding, len(claims.Roles))
	for i, r := range claims.Roles {
		out[i] = authz.RoleBinding{
			RoleName:  r,
			ScopeType: authz.ScopeTenant,
		}
	}
	return out
}
