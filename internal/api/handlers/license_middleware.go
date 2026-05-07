package handlers

// license_middleware.go — E24-C Open-core enterprise feature gate.
//
// 사용법:
//
//	r.Group(func(r chi.Router) {
//	    r.Use(h.RequireLicensedFeature(license.FeatureSSO))
//	    r.Get("/api/v1/auth/sso/{providerId}/login", h.StartSSOLogin)
//	    ...
//	})
//
// 응답: 라이선스 부재·만료·feature 미라이선스 시 402 Payment Required + JSON.
// (HTTP 402는 RFC 9110에서 "reserved" 상태이지만 SaaS 결제 게이트로 관행적 사용.)

import (
	"net/http"

	"github.com/ssabro/rosshield/internal/platform/license"
)

// RequireLicensedFeature는 enterprise feature 진입 게이트 미들웨어를 반환합니다.
//
// h.deps.License가 nil이거나 community SKU면 모든 요청 거부 (402).
// 라이선스가 enterprise + 해당 feature 라이선스됨이면 통과.
func (h *Handlers) RequireLicensedFeature(feature license.Feature) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := h.deps.License.CheckFeature(feature)
			if !result.Allowed {
				writeError(w, http.StatusPaymentRequired, "license required: "+result.Reason)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
