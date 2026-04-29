package handlers

// middleware.go — JWT auth 미들웨어 (E9 Stage B).
//
// Authorization: Bearer <jwt> 헤더에서 access token 추출 → tenant.Service.VerifyAccessToken →
// AccessClaims를 ctx에 주입 + storage.WithTenantID로 tx 진입점에서 자동 사용.
//
// 401 매핑:
//   - 헤더 부재
//   - 잘못된 포맷 (Bearer prefix 누락)
//   - 토큰 검증 실패 (만료·서명 불일치·malformed)

import (
	"context"
	"net/http"
	"strings"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// authCtxKey는 ctx에 AccessClaims를 저장하기 위한 unexported 키입니다.
type authCtxKey int

const claimsCtxKey authCtxKey = iota + 1

// claimsFromContext는 미들웨어가 주입한 AccessClaims를 ctx에서 추출합니다.
//
// 핸들러는 본 함수로만 claims에 접근 — 직접 ctx.Value 접근 금지.
// 미들웨어 미적용 path에서는 zero-value(빈 Subject) 반환.
func claimsFromContext(ctx context.Context) (tenant.AccessClaims, bool) {
	v, ok := ctx.Value(claimsCtxKey).(tenant.AccessClaims)
	return v, ok
}

// AuthMiddleware는 protected endpoint 앞단에 적용되는 JWT 검증 미들웨어입니다.
//
// 절차:
//  1. Authorization 헤더 추출 → "Bearer " prefix 검증
//  2. tenant.Service.VerifyAccessToken으로 stateless 검증
//  3. AccessClaims 추출 → ctx에 주입 + storage.WithTenantID
//  4. next.ServeHTTP 호출
//
// 실패 시 401 + JSON `{"error": "..."}` 형식. body는 짧은 영문 — 토큰 내용 노출 금지.
func (h *Handlers) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}

		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			writeError(w, http.StatusUnauthorized, "Authorization header must start with 'Bearer '")
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, bearerPrefix)
		if tokenStr == "" {
			writeError(w, http.StatusUnauthorized, "empty bearer token")
			return
		}

		claims, err := h.deps.Tenant.VerifyAccessToken(r.Context(), tokenStr)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		// ctx에 claims + tenantID 주입.
		// storage.WithTenantID로 후속 storage.Tx 진입점이 자동으로 tenant scope 적용.
		ctx := context.WithValue(r.Context(), claimsCtxKey, claims)
		ctx = storage.WithTenantID(ctx, claims.TenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
