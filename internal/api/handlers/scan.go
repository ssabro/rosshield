package handlers

// scan.go — POST /api/v1/scans 핸들러 (E9 Stage B + E12 Stage 8).
//
// 요청 본문: {"fleetId": "...", "packId": "...", "trigger": "manual"}
// 응답 201: {"sessionId": "scan_...", "status": "pending", ...}
//
// E12 Stage 8: pending session INSERT 후 비동기 goroutine으로 Orchestrator.Run 호출 —
// 호출자에게 즉시 sessionId 반환 + background에서 fleet의 robots × pack의 checks 실 cycle.
// ScanRun 결선이 nil이면 (Phase 1 호환) async trigger 생략.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ssabro/rosshield/internal/api/gen"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// startScanRequest는 POST /api/v1/scans 요청 본문입니다.
type startScanRequest struct {
	FleetID string `json:"fleetId"`
	PackID  string `json:"packId"`
	Trigger string `json:"trigger,omitempty"` // 빈 값이면 manual
	Total   int    `json:"total,omitempty"`   // 옵션 — Orchestrator가 산출하지만 본 Stage는 외부 입력
}

// scanSessionResponse는 응답에 포함되는 ScanSession 메타입니다.
type scanSessionResponse struct {
	SessionID string `json:"sessionId"`
	TenantID  string `json:"tenantId"`
	FleetID   string `json:"fleetId"`
	PackID    string `json:"packId"`
	Trigger   string `json:"trigger"`
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
}

// CreateScan은 POST /api/v1/scans 핸들러입니다.
//
// 검증:
//   - fleetId 필수 → 400 "missing fleetId"
//   - packId 필수 → 400 "missing packId"
//   - 도메인 ErrFleetNotFound·ErrPackNotFound → 400 (FK 위반)
//   - 라이선스 scans/day quota 초과 → 402 (E24-D)
//   - 그 외 도메인 에러 → 500
func (h *Handlers) CreateScan(w http.ResponseWriter, r *http.Request, _ gen.CreateScanParams) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	// E24-D — 라이선스 scans/day quota 게이트. enforcer nil(community SKU)면 즉시 통과.
	if h.deps.License != nil {
		quotaResult, err := h.deps.License.CheckScansToday(r.Context(), string(tenantID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "license quota check failed")
			return
		}
		if !quotaResult.Allowed {
			writeQuotaError(w, quotaResult)
			return
		}
	}

	var req startScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.FleetID == "" {
		writeError(w, http.StatusBadRequest, "missing fleetId")
		return
	}
	if req.PackID == "" {
		writeError(w, http.StatusBadRequest, "missing packId")
		return
	}

	trigger := scan.SessionTrigger(req.Trigger)
	if trigger == "" {
		trigger = scan.TriggerManual
	}

	// E12 Stage 8 — ScanRun 결선이 있으면 Total 산출용 robots+checks 사전 fetch.
	// 결선 없으면(Phase 1 호환) req.Total 그대로 사용.
	totalForSession := req.Total
	var (
		preloadedRobots []scan.RobotTarget
		preloadedChecks []scan.CheckDef
	)
	if h.deps.ScanRun != nil && h.deps.Robot != nil && h.deps.Benchmark != nil {
		robots, checks, err := h.preloadRobotsAndChecks(r.Context(), req.FleetID, req.PackID)
		if err == nil {
			totalForSession = len(robots) * len(checks)
			preloadedRobots = robots
			preloadedChecks = checks
		}
	}

	var session scan.ScanSession
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		s, e := h.deps.Scan.StartScan(ctx, tx, scan.StartScanRequest{
			FleetID: req.FleetID,
			PackID:  req.PackID,
			Trigger: trigger,
			Total:   totalForSession,
		})
		if e != nil {
			return e
		}
		session = s
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, scan.ErrSessionEmptyFleet),
			errors.Is(err, scan.ErrSessionEmptyPack),
			errors.Is(err, scan.ErrSessionInvalidTrigger),
			errors.Is(err, scan.ErrSessionNegativeTotal),
			errors.Is(err, scan.ErrFleetNotFound),
			errors.Is(err, scan.ErrPackNotFound):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, errorStatusFor(err), "start scan failed")
		}
		return
	}

	// E12 Stage 8 — ScanRun 결선되어 있으면 비동기 goroutine으로 Orchestrator.Run trigger.
	// nil 결선(Phase 1 호환)이면 pending 상태만 유지 + 외부 trigger 기다림.
	if h.deps.ScanRun != nil {
		go h.triggerScanRun(tenantID, session, preloadedRobots, preloadedChecks)
	}

	writeJSON(w, http.StatusCreated, scanSessionResponse{
		SessionID: session.ID,
		TenantID:  string(session.TenantID),
		FleetID:   session.FleetID,
		PackID:    session.PackID,
		Trigger:   string(session.Trigger),
		Status:    string(session.Status),
		Total:     session.Progress.Total,
		Completed: session.Progress.Completed,
		Failed:    session.Progress.Failed,
	})
}

// preloadRobotsAndChecks는 fleet의 robots × pack의 checks를 fetch해
// scan.RobotTarget·CheckDef로 매핑합니다. CreateScan handler가 Total 산출 + 비동기
// trigger에 전달하기 위해 동기 호출.
func (h *Handlers) preloadRobotsAndChecks(ctx context.Context, fleetID, packID string) ([]scan.RobotTarget, []scan.CheckDef, error) {
	var (
		targets []scan.RobotTarget
		checks  []scan.CheckDef
	)
	err := h.deps.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		rs, e := h.deps.Robot.ListRobots(ctx, tx, fleetID)
		if e != nil {
			return e
		}
		for _, r := range rs {
			targets = append(targets, scan.RobotTarget{
				RobotID:      r.ID,
				Host:         r.Host,
				Port:         r.Port,
				AuthType:     string(r.AuthType),
				CredentialID: r.CredentialID,
			})
		}
		p, e := h.deps.Benchmark.GetPackByID(ctx, tx, packID)
		if e != nil {
			return e
		}
		for _, c := range p.Checks {
			checks = append(checks, scan.CheckDef{
				PackCheckID:  c.ID,
				Code:         c.CheckID,
				AuditCommand: []string{"bash", "-c", c.AuditCommand},
				TimeoutSec:   scan.DefaultCheckTimeoutSec,
				EvalRuleJSON: c.EvaluationRule,
			})
		}
		return nil
	})
	return targets, checks, err
}

// triggerScanRun은 비동기 goroutine으로 Orchestrator.Run을 호출합니다.
//
// handler가 미리 fetch한 robots+checks를 받아 추가 DB 호출 없이 진입.
// 에러는 silent — Run 자체가 audit emit·event publish.
//
// ctx는 background — handler request ctx는 응답 후 cancel되므로 사용 X.
func (h *Handlers) triggerScanRun(tenantID storage.TenantID, session scan.ScanSession,
	targets []scan.RobotTarget, checks []scan.CheckDef) {
	if len(targets) == 0 || len(checks) == 0 {
		return
	}
	ctx := storage.WithTenantID(context.Background(), tenantID)
	_ = h.deps.ScanRun.Run(ctx, tenantID, session.ID, targets, checks)
}
