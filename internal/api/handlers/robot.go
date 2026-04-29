package handlers

// robot.go — GET /api/v1/robots 핸들러 (E9 Stage B).
//
// AuthMiddleware가 ctx에 TenantID 주입 → Tx에서 자동 격리.
// fleetId query 파라미터는 옵션 — 빈 값이면 tenant 전체 robot 반환.

import (
	"context"
	"net/http"

	"github.com/ssabro/rosshield/internal/api/gen"
	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// robotResponse는 응답에 포함되는 robot 메타입니다.
type robotResponse struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenantId"`
	FleetID     string   `json:"fleetId"`
	Name        string   `json:"name"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	AuthType    string   `json:"authType"`
	OSDistro    string   `json:"osDistro,omitempty"`
	ROSDistro   string   `json:"rosDistro,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Role        string   `json:"role,omitempty"`
	Criticality string   `json:"criticality"`
}

// listRobotsResponse는 GET /api/v1/robots 응답 본문입니다.
type listRobotsResponse struct {
	Robots []robotResponse `json:"robots"`
}

// ListRobots는 GET /api/v1/robots 핸들러입니다.
//
// AuthMiddleware가 사전에 ctx에 TenantID를 주입한 상태에서만 호출됨 — 직접 호출 시 401.
func (h *Handlers) ListRobots(w http.ResponseWriter, r *http.Request, params gen.ListRobotsParams) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	fleetID := ""
	if params.FleetId != nil {
		fleetID = *params.FleetId
	}

	var robots []robot.Robot
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		rs, e := h.deps.Robot.ListRobots(ctx, tx, fleetID)
		if e != nil {
			return e
		}
		robots = rs
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "list robots failed")
		return
	}

	out := listRobotsResponse{Robots: make([]robotResponse, 0, len(robots))}
	for _, rb := range robots {
		out.Robots = append(out.Robots, robotResponse{
			ID:          rb.ID,
			TenantID:    string(rb.TenantID),
			FleetID:     rb.FleetID,
			Name:        rb.Name,
			Host:        rb.Host,
			Port:        rb.Port,
			AuthType:    string(rb.AuthType),
			OSDistro:    rb.OSDistro,
			ROSDistro:   rb.ROSDistro,
			Tags:        rb.Tags,
			Role:        rb.Role,
			Criticality: string(rb.Criticality),
		})
	}
	writeJSON(w, http.StatusOK, out)
}
