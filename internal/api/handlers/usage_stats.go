package handlers

import (
	"net/http"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// usageStatsResponse는 한 tenant의 누적 사용 통계입니다.
//
// 본 응답은 Prometheus counter 카운트의 process scope 스냅샷입니다 — process restart 시
// 0부터 다시 카운트. 정확한 누적은 외부 Prometheus + Grafana 권장.
//
// scansCompleted는 status별 분포 (completed|failed|cancelled). violationRate는
// scanFailedChecks / sum(scansCompleted) — 클라이언트가 계산 가능하지만 server에서도 제공
// (운영자 dashboard 즉시 표시).
type usageStatsResponse struct {
	Tenant            string             `json:"tenant"`
	ScansStarted      float64            `json:"scansStarted"`
	ScansCompleted    map[string]float64 `json:"scansCompleted"`
	ScanFailedChecks  float64            `json:"scanFailedChecks"`
	ScansCompletedSum float64            `json:"scansCompletedSum"` // status 전체 합 — UI에서 분모로 활용
}

// GetUsageStats는 GET /api/v1/usage/stats handler.
//
// 응답: 현재 인증된 tenant의 누적 카운트. metrics registry 미설정 시 503.
// 모든 인증 사용자가 read 가능 (read-only, sensitive data 없음 — 카운트만).
func (h *Handlers) GetUsageStats(w http.ResponseWriter, r *http.Request) {
	if h.deps.Metrics == nil {
		writeError(w, http.StatusServiceUnavailable, "metrics registry not configured")
		return
	}
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}

	u := h.deps.Metrics.GetTenantUsage(string(tenantID))

	var completedSum float64
	for _, v := range u.ScansCompleted {
		completedSum += v
	}

	writeJSON(w, http.StatusOK, usageStatsResponse{
		Tenant:            string(tenantID),
		ScansStarted:      u.ScansStarted,
		ScansCompleted:    u.ScansCompleted,
		ScanFailedChecks:  u.ScanFailedChecks,
		ScansCompletedSum: completedSum,
	})
}
