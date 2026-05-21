package handlers

// compliance_effectiveness.go — Phase 11.B-6 SOC2 통제 effectiveness dashboard backend.
//
// GET /api/v1/compliance/effectiveness — SOC2 통제 매트릭스 cover% rollup +
// 카테고리별 audit event 1d/7d/30d 집계. design doc
// `docs/design/notes/soc2-readiness-design.md` §7.6.
//
// 권한 (handlers.go 라우터): ResourceAudit.ActionExport (permission_matrix.go §3.3 —
// admin + auditor 통과). compliance.read 는 너무 broad (operator/fleet-admin/read-only
// 까지 통과) — effectiveness dashboard 는 외부 감사인 위임 표면이므로 audit.export
// 권한과 동일 게이트가 적절.
//
// 응답 구조:
//
//   {
//     "totalSubControls": 40,
//     "coveredSubControls": 33,
//     "coverPercent": 82.5,
//     "generatedAt": "2026-05-21T12:00:00Z",
//     "categories": [
//       { "code": "CC1", "name": "...", "subControls": 5, "covered": 3,
//         "coverPercent": 60.0, "auditEvents": {"lastDay": 0, "last7Days": 5, "last30Days": 18},
//         "gaps": ["CC1.2 Board Oversight", "CC1.4 Commitment to Competence"],
//         "items": [{ "id":"CC1.1", "title":"...", "covered":true, "lastDay":0, ... }, ...] },
//       ...
//     ]
//   }
//
// audit emit: 0 (read-only — design doc §7.6 + memory `feedback_skip_handoff.md` 일관).
//
// 503 조건: AuditEffectiveness 미주입 (옵트인 게이트 — Phase 11.B-6 미활성 환경 안전).

import (
	"context"
	"net/http"

	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// effectivenessResponse 는 GET /api/v1/compliance/effectiveness 응답 본문입니다.
type effectivenessResponse struct {
	TotalSubControls   int                             `json:"totalSubControls"`
	CoveredSubControls int                             `json:"coveredSubControls"`
	CoverPercent       float64                         `json:"coverPercent"`
	GeneratedAt        string                          `json:"generatedAt"`
	Categories         []effectivenessCategoryResponse `json:"categories"`
}

type effectivenessCategoryResponse struct {
	Code         string                            `json:"code"`
	Name         string                            `json:"name"`
	SubControls  int                               `json:"subControls"`
	Covered      int                               `json:"covered"`
	CoverPercent float64                           `json:"coverPercent"`
	AuditEvents  effectivenessAuditEventCounts     `json:"auditEvents"`
	Gaps         []string                          `json:"gaps"`
	Items        []effectivenessSubControlResponse `json:"items"`
}

type effectivenessAuditEventCounts struct {
	LastDay    int64 `json:"lastDay"`
	Last7Days  int64 `json:"last7Days"`
	Last30Days int64 `json:"last30Days"`
}

type effectivenessSubControlResponse struct {
	ID          string                        `json:"id"`
	Title       string                        `json:"title"`
	Actions     []string                      `json:"actions"`
	Covered     bool                          `json:"covered"`
	GapNote     string                        `json:"gapNote,omitempty"`
	AuditEvents effectivenessAuditEventCounts `json:"auditEvents"`
}

// GetComplianceEffectiveness 는 GET /api/v1/compliance/effectiveness 핸들러입니다.
//
// 흐름:
//
//  1. tenant context 추출 (auth middleware 가 주입).
//  2. compliance.MappedActions() 로 audit event action 셋 회수.
//  3. AuditEffectiveness.CountActionsByWindows 로 단일 query 집계.
//  4. compliance.BuildEffectivenessDashboard 로 cover% rollup 빌드.
//  5. JSON 직렬화.
//
// 의존성 미주입(AuditEffectiveness == nil) 이면 503 — Phase 11.B-6 옵트인 게이트.
func (h *Handlers) GetComplianceEffectiveness(w http.ResponseWriter, r *http.Request) {
	if h.deps.AuditEffectiveness == nil {
		writeError(w, http.StatusServiceUnavailable, "compliance effectiveness not configured")
		return
	}

	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	actions := compliance.MappedActions()
	now := h.deps.Clock.Now().UTC()

	counts := make(map[string]compliance.ActionCounts, len(actions))
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		rows, e := h.deps.AuditEffectiveness.CountActionsByWindows(ctx, tx, tenantID, actions, now)
		if e != nil {
			return e
		}
		for _, row := range rows {
			counts[row.Action] = compliance.ActionCounts{
				LastDay:    row.LastDay,
				Last7Days:  row.Last7Days,
				Last30Days: row.Last30Days,
			}
		}
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "compliance effectiveness aggregate failed")
		return
	}

	dash := compliance.BuildEffectivenessDashboard(counts, now)
	resp := effectivenessResponse{
		TotalSubControls:   dash.TotalSubControls,
		CoveredSubControls: dash.CoveredSubControls,
		CoverPercent:       roundCoverPercent(dash.CoverPercent),
		GeneratedAt:        dash.GeneratedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
		Categories:         make([]effectivenessCategoryResponse, 0, len(dash.Categories)),
	}
	for _, cat := range dash.Categories {
		catResp := effectivenessCategoryResponse{
			Code:         cat.Code,
			Name:         cat.Name,
			SubControls:  cat.SubControls,
			Covered:      cat.Covered,
			CoverPercent: roundCoverPercent(cat.CoverPercent),
			AuditEvents: effectivenessAuditEventCounts{
				LastDay:    cat.LastDay,
				Last7Days:  cat.Last7Days,
				Last30Days: cat.Last30Days,
			},
			Gaps:  cat.Gaps,
			Items: make([]effectivenessSubControlResponse, 0, len(cat.Items)),
		}
		if catResp.Gaps == nil {
			catResp.Gaps = []string{}
		}
		for _, it := range cat.Items {
			actionsOut := it.Actions
			if actionsOut == nil {
				actionsOut = []string{}
			}
			catResp.Items = append(catResp.Items, effectivenessSubControlResponse{
				ID:      it.ID,
				Title:   it.Title,
				Actions: actionsOut,
				Covered: it.Covered,
				GapNote: it.GapNote,
				AuditEvents: effectivenessAuditEventCounts{
					LastDay:    it.LastDay,
					Last7Days:  it.Last7Days,
					Last30Days: it.Last30Days,
				},
			})
		}
		resp.Categories = append(resp.Categories, catResp)
	}

	writeJSON(w, http.StatusOK, resp)
}

// roundCoverPercent 는 cover% 를 소수점 1 자리로 반올림합니다 (UI 표시 안정성).
func roundCoverPercent(p float64) float64 {
	return float64(int(p*10+0.5)) / 10.0
}
