package handlers

// license_info.go — GET /api/v1/license (B5 Web Console 지원).
//
// 응답: 라이선스 메타(없으면 community 기본값). 토큰 자체는 노출하지 않음.
// 모든 인증된 사용자가 조회 가능 — 운영자가 만료·feature·quota 확인용.

import (
	"net/http"

	"github.com/ssabro/rosshield/internal/platform/license"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// licenseInfoResponse는 GET /api/v1/license 응답 본문입니다.
//
// Token 자체나 서명 같은 민감 정보는 노출하지 않음 — 사용자가 알아야 하는 메타만.
type licenseInfoResponse struct {
	Edition   string             `json:"edition"`
	IssuedTo  string             `json:"issuedTo,omitempty"`
	IssuedAt  string             `json:"issuedAt,omitempty"`
	ExpiresAt string             `json:"expiresAt,omitempty"`
	Expired   bool               `json:"expired"`
	Features  []string           `json:"features,omitempty"`
	Quotas    licenseQuotaPublic `json:"quotas"`
}

type licenseQuotaPublic struct {
	RobotsMax       int `json:"robotsMax"`       // 0 = 무제한
	ScansPerDay     int `json:"scansPerDay"`     // 0 = 무제한
	LLMTokensPerDay int `json:"llmTokensPerDay"` // 0 = 무제한
}

// GetLicenseInfo는 GET /api/v1/license 핸들러입니다.
//
// 인증된 사용자만 호출 가능. License 미설정 시 community 기본값 응답(에러 X).
func (h *Handlers) GetLicenseInfo(w http.ResponseWriter, r *http.Request) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	resp := licenseInfoResponse{Edition: string(license.EditionCommunity)}
	if h.deps.License != nil {
		p := h.deps.License.Payload()
		if p.Version != 0 {
			resp.Edition = string(p.Edition)
			resp.IssuedTo = p.IssuedTo
			if !p.IssuedAt.IsZero() {
				resp.IssuedAt = p.IssuedAt.UTC().Format("2006-01-02T15:04:05Z")
			}
			if !p.ExpiresAt.IsZero() {
				resp.ExpiresAt = p.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
				resp.Expired = p.IsExpired(h.deps.Clock.Now())
			}
			resp.Features = make([]string, 0, len(p.Features))
			for _, f := range p.Features {
				resp.Features = append(resp.Features, string(f))
			}
			resp.Quotas.RobotsMax = p.Quotas.RobotsMax
			resp.Quotas.ScansPerDay = p.Quotas.ScansPerDay
			resp.Quotas.LLMTokensPerDay = p.Quotas.LLMTokensPerDay
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
