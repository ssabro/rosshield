package handlers

// insight.go — Insight 도메인 HTTP 표면 (E17 Phase 2).
//
// 엔드포인트 3종 — gen.ServerInterface 메서드를 직접 구현:
//
//	GET  /api/v1/insights                          → ListInsights (kind/severity/robotId 필터)
//	POST /api/v1/insights/{insightId}:dismiss      → DismissInsight (reason body)
//	POST /api/v1/fleets/{fleetId}/insights:run     → RunFleetInsights (drift·anomaly·peer 수동 트리거)
//
// 도메인 결합 (P5): handlers는 insight.Service interface만 호출. tenant scope는
// AuthMiddleware가 ctx에 주입한 후 storage.Tx에서 자동 적용.

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/ssabro/rosshield/internal/api/gen"
	"github.com/ssabro/rosshield/internal/domain/insight"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// insightResponse는 응답에 포함되는 insight 메타입니다.
type insightResponse struct {
	ID           string   `json:"id"`
	TenantID     string   `json:"tenantId"`
	Kind         string   `json:"kind"`
	Severity     string   `json:"severity"`
	RobotID      string   `json:"robotId,omitempty"`
	FleetID      string   `json:"fleetId,omitempty"`
	CheckID      string   `json:"checkId,omitempty"`
	Summary      string   `json:"summary"`
	Reasoning    string   `json:"reasoning,omitempty"`
	RulesApplied []string `json:"rulesApplied,omitempty"`
	Confidence   float64  `json:"confidence"`
	ProducedBy   string   `json:"producedBy"`
	CreatedAt    string   `json:"createdAt"`
	DismissedAt  string   `json:"dismissedAt,omitempty"`
	DismissedBy  string   `json:"dismissedBy,omitempty"`
}

type listInsightsResponse struct {
	Insights []insightResponse `json:"insights"`
}

type runInsightsResponse struct {
	Produced []insightResponse `json:"produced"`
	Count    int               `json:"count"`
}

// ListInsights는 GET /api/v1/insights 핸들러입니다.
func (h *Handlers) ListInsights(w http.ResponseWriter, r *http.Request, params gen.ListInsightsParams) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	filter := insight.ListFilter{}
	if params.Kind != nil {
		filter.Kind = insight.Kind(*params.Kind)
	}
	if params.Severity != nil {
		filter.Severity = insight.Severity(*params.Severity)
	}
	if params.RobotId != nil {
		filter.RobotID = *params.RobotId
	}
	if params.Limit != nil {
		filter.Limit = *params.Limit
	}

	var ins []insight.Insight
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Insight.ListActive(ctx, tx, filter)
		if e != nil {
			return e
		}
		ins = out
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "list insights failed")
		return
	}

	out := listInsightsResponse{Insights: make([]insightResponse, 0, len(ins))}
	for _, in := range ins {
		out.Insights = append(out.Insights, mapInsight(in))
	}
	writeJSON(w, http.StatusOK, out)
}

// DismissInsight는 POST /api/v1/insights/{insightId}:dismiss 핸들러입니다.
//
// dismissedBy는 ctx의 인증된 user(AccessClaims.Subject) — 미들웨어 통과 후이므로 항상 채워짐.
func (h *Handlers) DismissInsight(w http.ResponseWriter, r *http.Request, insightID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok || claims.Subject == "" {
		writeError(w, http.StatusUnauthorized, "no user in context")
		return
	}

	var body gen.DismissInsightJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}

	var dismissed insight.Insight
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Insight.Dismiss(ctx, tx, insightID, claims.Subject, body.Reason)
		if e != nil {
			return e
		}
		dismissed = out
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "dismiss insight failed")
		return
	}
	writeJSON(w, http.StatusOK, mapInsight(dismissed))
}

// RunFleetInsights는 POST /api/v1/fleets/{fleetId}/insights:run 핸들러입니다.
//
// E19에서 scan.completed 이벤트 자동화 전까지의 수동 트리거 — 이후에도 on-demand recompute로 유지.
func (h *Handlers) RunFleetInsights(w http.ResponseWriter, r *http.Request, fleetID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if fleetID == "" {
		writeError(w, http.StatusBadRequest, "fleetId is required")
		return
	}

	var produced []insight.Insight
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Insight.RunForFleet(ctx, tx, fleetID)
		if e != nil {
			return e
		}
		produced = out
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "run fleet insights failed")
		return
	}

	out := runInsightsResponse{
		Produced: make([]insightResponse, 0, len(produced)),
		Count:    len(produced),
	}
	for _, in := range produced {
		out.Produced = append(out.Produced, mapInsight(in))
	}
	writeJSON(w, http.StatusOK, out)
}

// mapInsight는 도메인 Insight를 응답 DTO로 변환합니다.
func mapInsight(in insight.Insight) insightResponse {
	resp := insightResponse{
		ID:           in.ID,
		TenantID:     string(in.TenantID),
		Kind:         string(in.Kind),
		Severity:     string(in.Severity),
		RobotID:      in.Scope.RobotID,
		FleetID:      in.Scope.FleetID,
		CheckID:      in.Scope.CheckID,
		Summary:      in.Summary,
		Reasoning:    in.Reasoning,
		RulesApplied: in.RulesApplied,
		Confidence:   in.Confidence,
		ProducedBy:   string(in.ProducedBy),
		CreatedAt:    in.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
		DismissedBy:  in.DismissedBy,
	}
	if in.DismissedAt != nil {
		resp.DismissedAt = in.DismissedAt.UTC().Format("2006-01-02T15:04:05.000000000Z")
	}
	return resp
}
