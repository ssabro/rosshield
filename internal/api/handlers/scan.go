package handlers

// scan.go — POST /api/v1/scans 핸들러 (E9 Stage B).
//
// 요청 본문: {"fleetId": "...", "packId": "...", "trigger": "manual"}
// 응답 201: {"sessionId": "scan_...", "status": "pending", ...}
//
// Phase 1 Stage B는 pending 상태로 INSERT만 — Orchestrator(scanrun) 시작은 후속 Stage에서.

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

	var session scan.ScanSession
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		s, e := h.deps.Scan.StartScan(ctx, tx, scan.StartScanRequest{
			FleetID: req.FleetID,
			PackID:  req.PackID,
			Trigger: trigger,
			Total:   req.Total,
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
