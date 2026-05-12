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
	"strconv"
	"time"

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
	SessionID     string  `json:"sessionId"`
	TenantID      string  `json:"tenantId"`
	FleetID       string  `json:"fleetId"`
	PackID        string  `json:"packId"`
	Trigger       string  `json:"trigger"`
	Status        string  `json:"status"`
	Total         int     `json:"total"`
	Completed     int     `json:"completed"`
	Failed        int     `json:"failed"`
	FailureReason string  `json:"failureReason,omitempty"`
	CreatedAt     string  `json:"createdAt,omitempty"`
	UpdatedAt     string  `json:"updatedAt,omitempty"`
	StartedAt     *string `json:"startedAt,omitempty"`
	CompletedAt   *string `json:"completedAt,omitempty"`
}

// toScanSessionResponse는 도메인 ScanSession을 응답 DTO로 변환합니다.
func toScanSessionResponse(s scan.ScanSession) scanSessionResponse {
	resp := scanSessionResponse{
		SessionID:     s.ID,
		TenantID:      string(s.TenantID),
		FleetID:       s.FleetID,
		PackID:        s.PackID,
		Trigger:       string(s.Trigger),
		Status:        string(s.Status),
		Total:         s.Progress.Total,
		Completed:     s.Progress.Completed,
		Failed:        s.Progress.Failed,
		FailureReason: s.FailureReason,
	}
	if !s.CreatedAt.IsZero() {
		resp.CreatedAt = s.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !s.UpdatedAt.IsZero() {
		resp.UpdatedAt = s.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if s.StartedAt != nil && !s.StartedAt.IsZero() {
		t := s.StartedAt.UTC().Format(time.RFC3339Nano)
		resp.StartedAt = &t
	}
	if s.CompletedAt != nil && !s.CompletedAt.IsZero() {
		t := s.CompletedAt.UTC().Format(time.RFC3339Nano)
		resp.CompletedAt = &t
	}
	return resp
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

	writeJSON(w, http.StatusCreated, toScanSessionResponse(session))
}

// cancelScanRequest는 POST /api/v1/scans/{sessionId}:cancel 요청 본문입니다.
//
// reason은 옵션 — 빈 값이면 "user requested" default. audit·event payload에 기록.
type cancelScanRequest struct {
	Reason string `json:"reason,omitempty"`
}

// CancelScan은 POST /api/v1/scans/{sessionId}:cancel 핸들러입니다.
//
// pending·running 둘 다 cancel 가능 → cancelled 전이.
// 이미 terminal(completed/failed/cancelled) 세션이면 409 Conflict.
// 미존재(또는 cross-tenant)는 404, auth 없으면 401.
func (h *Handlers) CancelScan(w http.ResponseWriter, r *http.Request, sessionID string) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing sessionId")
		return
	}

	var req cancelScanRequest
	// 본문은 옵션 — 빈 본문 허용. JSON parse 실패 시 무시(reason 빈 값으로 진행).
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	reason := req.Reason
	if reason == "" {
		reason = "user requested"
	}

	var (
		session scan.ScanSession
		err     error
	)
	// ScanRun 결선이 있으면 in-flight ctx 취소 + DB 전이 한 번에 위임 (cooperative shutdown).
	// 결선 없으면 (Phase 1 호환) DB-only cancel.
	if h.deps.ScanRun != nil {
		session, err = h.deps.ScanRun.Cancel(r.Context(), tenantID, sessionID, reason)
	} else {
		err = h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
			s, e := h.deps.Scan.CancelSession(ctx, tx, sessionID, reason)
			if e != nil {
				return e
			}
			session = s
			return nil
		})
	}
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFound):
			writeError(w, http.StatusNotFound, "scan session not found")
		case errors.Is(err, scan.ErrInvalidTransition):
			writeError(w, http.StatusConflict, "scan session already in terminal state")
		default:
			writeError(w, errorStatusFor(err), "cancel scan failed")
		}
		return
	}

	writeJSON(w, http.StatusOK, toScanSessionResponse(session))
}

// scanListResponse는 GET /api/v1/scans 응답 envelope입니다.
type scanListResponse struct {
	Sessions []scanSessionResponse `json:"sessions"`
}

// listScansFilter는 query string에서 추출한 필터입니다.
type listScansFilter struct {
	FleetID string
	Status  string
	Limit   int
}

// ListScans는 GET /api/v1/scans 핸들러입니다.
//
// tenant scope 세션을 created_at DESC로 반환. 옵션 query: status·fleetId·limit.
// limit 미지정 시 도메인 default(50). 최대 limit 200으로 cap.
func (h *Handlers) ListScans(w http.ResponseWriter, r *http.Request, _ gen.ListScansParams) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	f := parseListScansFilter(r)
	domainFilter := scan.ListSessionsFilter{
		FleetID: f.FleetID,
		Status:  scan.SessionStatus(f.Status),
		Limit:   f.Limit,
	}

	var sessions []scan.ScanSession
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		ss, e := h.deps.Scan.ListSessions(ctx, tx, domainFilter)
		if e != nil {
			return e
		}
		sessions = ss
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "list scans failed")
		return
	}

	out := scanListResponse{Sessions: make([]scanSessionResponse, 0, len(sessions))}
	for _, s := range sessions {
		out.Sessions = append(out.Sessions, toScanSessionResponse(s))
	}
	writeJSON(w, http.StatusOK, out)
}

// parseListScansFilter는 query string에서 listScansFilter를 추출합니다.
//
// limit 200 cap (DoS 방어). 잘못된 limit 값은 무시(default 위임).
func parseListScansFilter(r *http.Request) listScansFilter {
	q := r.URL.Query()
	f := listScansFilter{
		FleetID: q.Get("fleetId"),
		Status:  q.Get("status"),
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 200 {
				n = 200
			}
			f.Limit = n
		}
	}
	return f
}

// GetScan은 GET /api/v1/scans/{sessionId} 핸들러입니다.
//
// tenant scope에서 단일 세션 조회 — Web UI 페이지 reload·polling fallback 용도.
// 미존재(또는 cross-tenant)는 404, auth 없으면 401, ctx 누락 401.
func (h *Handlers) GetScan(w http.ResponseWriter, r *http.Request, sessionID string) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing sessionId")
		return
	}

	var session scan.ScanSession
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		s, e := h.deps.Scan.GetSession(ctx, tx, sessionID)
		if e != nil {
			return e
		}
		session = s
		return nil
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "scan session not found")
			return
		}
		writeError(w, errorStatusFor(err), "get scan failed")
		return
	}

	writeJSON(w, http.StatusOK, toScanSessionResponse(session))
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
