package handlers

// robot.go — GET/POST /api/v1/robots 핸들러.
//
// AuthMiddleware가 ctx에 TenantID 주입 → Tx에서 자동 격리.
// fleetId query 파라미터는 옵션 — 빈 값이면 tenant 전체 robot 반환.
// CreateRobot은 운영 e2e 갭(A1) 회수 — robot.Service.CreateRobot 결선.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ssabro/rosshield/internal/api/gen"
	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/scan"
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
		out.Robots = append(out.Robots, toRobotResponse(rb))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetRobot는 GET /api/v1/robots/{robotId} 핸들러입니다.
//
// tenant scope 단일 robot 조회 — 모든 인증 사용자 read.
// 미존재(또는 cross-tenant, soft-deleted) → 404. 응답에 평문 자격증명은 포함 X (보안).
func (h *Handlers) GetRobot(w http.ResponseWriter, r *http.Request, robotID string) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if robotID == "" {
		writeError(w, http.StatusBadRequest, "missing robotId")
		return
	}
	if h.deps.Robot == nil {
		writeError(w, http.StatusServiceUnavailable, "robot service not configured")
		return
	}

	var rb robot.Robot
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		got, e := h.deps.Robot.GetRobot(ctx, tx, robotID)
		if e != nil {
			return e
		}
		rb = got
		return nil
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "robot not found")
			return
		}
		writeError(w, errorStatusFor(err), "get robot failed")
		return
	}
	writeJSON(w, http.StatusOK, toRobotResponse(rb))
}

// robotResultResponse는 GET /api/v1/robots/{robotId}/results 항목입니다.
type robotResultResponse struct {
	ID                   string `json:"id"`
	SessionID            string `json:"sessionId"`
	CheckID              string `json:"checkId"`
	PackCheckID          string `json:"packCheckId"`
	PackKey              string `json:"packKey,omitempty"`              // derived: session→pack JOIN, check navigation 용
	SessionStartedAt     string `json:"sessionStartedAt,omitempty"`     // derived: scan_sessions.started_at JOIN
	SessionCompletedAt   string `json:"sessionCompletedAt,omitempty"`   // derived: scan_sessions.completed_at JOIN
	SessionFailureReason string `json:"sessionFailureReason,omitempty"` // derived: scan_sessions.failure_reason JOIN
	SessionStatus        string `json:"sessionStatus,omitempty"`        // derived: scan_sessions.status JOIN
	Outcome              string `json:"outcome"`
	EvalReason           string `json:"evalReason,omitempty"`
	DurationMs           int64  `json:"durationMs"`
	ExecutedAt           string `json:"executedAt"`
	CreatedAt            string `json:"createdAt"`
}

// robotResultsListResponse는 GET /api/v1/robots/{robotId}/results 응답 envelope입니다.
type robotResultsListResponse struct {
	Results []robotResultResponse `json:"results"`
}

// ListRobotResults는 GET /api/v1/robots/{robotId}/results 핸들러입니다.
//
// robot scope 최근 scan results를 executed_at DESC로 반환. limit query 옵션(default 20, 200 cap).
// 모든 인증 사용자 read.
func (h *Handlers) ListRobotResults(w http.ResponseWriter, r *http.Request, robotID string) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if robotID == "" {
		writeError(w, http.StatusBadRequest, "missing robotId")
		return
	}
	if h.deps.Scan == nil {
		writeError(w, http.StatusServiceUnavailable, "scan service not configured")
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 200 {
				n = 200
			}
			limit = n
		}
	}

	var results []scan.ScanResult
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		rs, e := h.deps.Scan.ListResultsByRobot(ctx, tx, robotID, limit)
		if e != nil {
			return e
		}
		results = rs
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "list robot results failed")
		return
	}

	out := robotResultsListResponse{Results: make([]robotResultResponse, 0, len(results))}
	for _, res := range results {
		item := robotResultResponse{
			ID:          res.ID,
			SessionID:   res.SessionID,
			CheckID:     res.CheckID,
			PackCheckID: res.PackCheckID,
			PackKey:     res.PackKey,
			Outcome:     string(res.Outcome),
			EvalReason:  res.EvalReason,
			DurationMs:  res.DurationMs,
		}
		if !res.ExecutedAt.IsZero() {
			item.ExecutedAt = res.ExecutedAt.UTC().Format(time.RFC3339Nano)
		}
		if !res.CreatedAt.IsZero() {
			item.CreatedAt = res.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		if res.SessionStartedAt != nil && !res.SessionStartedAt.IsZero() {
			item.SessionStartedAt = res.SessionStartedAt.UTC().Format(time.RFC3339Nano)
		}
		if res.SessionCompletedAt != nil && !res.SessionCompletedAt.IsZero() {
			item.SessionCompletedAt = res.SessionCompletedAt.UTC().Format(time.RFC3339Nano)
		}
		item.SessionFailureReason = res.SessionFailureReason
		item.SessionStatus = string(res.SessionStatusEnriched)
		out.Results = append(out.Results, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// rotateCredentialRequest는 POST /api/v1/robots/{robotId}/credential:rotate 본문입니다.
//
// 평문 자격증명 — 메모리에서만 처리, 도메인 layer가 KEK→DEK로 wrap.
type rotateCredentialRequest struct {
	AuthType             string `json:"authType"` // "password" | "privateKey"
	Username             string `json:"username"`
	Password             string `json:"password,omitempty"`
	PrivateKeyPEM        string `json:"privateKeyPem,omitempty"`
	PrivateKeyPassphrase string `json:"privateKeyPassphrase,omitempty"`
}

// rotateCredentialResponse는 응답 — 평문 미포함, 새/이전 credentialID만.
type rotateCredentialResponse struct {
	NewCredentialID string `json:"newCredentialId"`
	OldCredentialID string `json:"oldCredentialId"`
}

// RotateCredential은 POST /api/v1/robots/{robotId}/credential:rotate 핸들러입니다 (admin only).
//
// 새 credential 생성 + robot.credential_id 갱신 + 이전 credential을 revoked_at으로 soft delete.
// audit emit (credential.rotated, R3-3).
func (h *Handlers) RotateCredential(w http.ResponseWriter, r *http.Request, robotID string) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if robotID == "" {
		writeError(w, http.StatusBadRequest, "missing robotId")
		return
	}
	if h.deps.Robot == nil {
		writeError(w, http.StatusServiceUnavailable, "robot service not configured")
		return
	}

	var req rotateCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	domainReq := robot.RotateCredentialRequest{
		RobotID: robotID,
		Material: robot.CredentialMaterial{
			Type:                 robot.CredentialType(req.AuthType),
			Username:             req.Username,
			Password:             req.Password,
			PrivateKeyPEM:        req.PrivateKeyPEM,
			PrivateKeyPassphrase: req.PrivateKeyPassphrase,
		},
	}

	var result robot.RotateCredentialResult
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		res, e := h.deps.Robot.RotateCredential(ctx, tx, domainReq)
		if e != nil {
			return e
		}
		result = res
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFound):
			writeError(w, http.StatusNotFound, "robot not found")
		case errors.Is(err, robot.ErrCredentialUnknownType),
			errors.Is(err, robot.ErrCredentialEmptyUser):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, errorStatusFor(err), "rotate credential failed")
		}
		return
	}

	writeJSON(w, http.StatusOK, rotateCredentialResponse{
		NewCredentialID: result.NewCredentialID,
		OldCredentialID: result.OldCredentialID,
	})
}

// DeleteRobot은 DELETE /api/v1/robots/{robotId} 핸들러입니다 (admin only — chi mount).
//
// soft delete (deleted_at = now). 미존재(이미 deleted, cross-tenant) → 404.
// 응답 204 No Content. 두 번째 호출도 404 (멱등 아님 — 명시적 한 번만, R3-5).
func (h *Handlers) DeleteRobot(w http.ResponseWriter, r *http.Request, robotID string) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if robotID == "" {
		writeError(w, http.StatusBadRequest, "missing robotId")
		return
	}
	if h.deps.Robot == nil {
		writeError(w, http.StatusServiceUnavailable, "robot service not configured")
		return
	}

	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		return h.deps.Robot.DeleteRobot(ctx, tx, robotID)
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "robot not found")
			return
		}
		writeError(w, errorStatusFor(err), "delete robot failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// createRobotRequest는 POST /api/v1/robots 요청 본문입니다.
//
// 평문 자격증명을 받음 — 메모리 전용 처리 후 도메인 layer가 KEK→DEK로 wrap.
// 응답은 평문 자격증명을 포함하지 않음 (Robot 메타 + Credential.ID만).
type createRobotRequest struct {
	FleetID              string   `json:"fleetId"`
	Name                 string   `json:"name"`
	Host                 string   `json:"host"`
	Port                 int      `json:"port,omitempty"` // 0이면 도메인이 default(22)
	AuthType             string   `json:"authType"`       // "password" | "privateKey"
	Username             string   `json:"username"`
	Password             string   `json:"password,omitempty"`
	PrivateKeyPEM        string   `json:"privateKeyPem,omitempty"`
	PrivateKeyPassphrase string   `json:"privateKeyPassphrase,omitempty"`
	OSDistro             string   `json:"osDistro,omitempty"`
	ROSDistro            string   `json:"rosDistro,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	Role                 string   `json:"role,omitempty"`
	Criticality          string   `json:"criticality,omitempty"` // 빈 값 → 도메인 default(medium)
}

// createRobotResponse는 응답 본문 — 평문 자격증명 미포함.
type createRobotResponse struct {
	Robot        robotResponse `json:"robot"`
	CredentialID string        `json:"credentialId"`
}

// CreateRobot은 POST /api/v1/robots 핸들러입니다 (gen.ServerInterface override).
//
// 검증:
//   - tenant 미주입 → 401
//   - 빈 JSON / 형식 오류 → 400
//   - 도메인 sentinel(ErrRobotEmptyName/ErrFleetNotFound 등) → 400
//   - 라이선스 robots quota 초과 → 402 (E24-D)
//   - 그 외 → 500
//
// 응답 201 — Location 헤더는 미설정 (chi 직접 mount 패턴 — 후속 표면 정리에서).
func (h *Handlers) CreateRobot(w http.ResponseWriter, r *http.Request, _ gen.CreateRobotParams) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	// E24-D — 라이선스 robots quota 게이트. enforcer nil(community SKU)면 즉시 통과.
	if h.deps.License != nil {
		quotaResult, err := h.deps.License.CheckRobotsAdd(r.Context(), string(tenantID), 1)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "license quota check failed")
			return
		}
		if !quotaResult.Allowed {
			writeQuotaError(w, quotaResult)
			return
		}
	}

	var req createRobotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	material := robot.CredentialMaterial{
		Type:                 robot.CredentialType(req.AuthType),
		Username:             req.Username,
		Password:             req.Password,
		PrivateKeyPEM:        req.PrivateKeyPEM,
		PrivateKeyPassphrase: req.PrivateKeyPassphrase,
	}

	domainReq := robot.CreateRobotRequest{
		FleetID:     req.FleetID,
		Name:        req.Name,
		Host:        req.Host,
		Port:        req.Port,
		AuthType:    robot.AuthType(req.AuthType),
		Material:    material,
		OSDistro:    req.OSDistro,
		ROSDistro:   req.ROSDistro,
		Tags:        req.Tags,
		Role:        req.Role,
		Criticality: robot.Criticality(req.Criticality),
	}

	var result robot.CreateRobotResult
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		res, e := h.deps.Robot.CreateRobot(ctx, tx, domainReq)
		if e != nil {
			return e
		}
		result = res
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, robot.ErrFleetNotFound),
			errors.Is(err, robot.ErrRobotEmptyName),
			errors.Is(err, robot.ErrRobotNameTooLong),
			errors.Is(err, robot.ErrRobotEmptyHost),
			errors.Is(err, robot.ErrRobotInvalidPort),
			errors.Is(err, robot.ErrRobotInvalidAuthType),
			errors.Is(err, robot.ErrRobotInvalidCritical),
			errors.Is(err, robot.ErrCredentialUnknownType),
			errors.Is(err, robot.ErrCredentialEmptyUser):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, robot.ErrRobotNameDuplicate),
			errors.Is(err, robot.ErrRobotHostPortConflict):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, errorStatusFor(err), "create robot failed")
		}
		return
	}

	writeJSON(w, http.StatusCreated, createRobotResponse{
		Robot:        toRobotResponse(result.Robot),
		CredentialID: result.Credential.ID,
	})
}

// toRobotResponse는 도메인 Robot을 응답 DTO로 변환합니다.
func toRobotResponse(rb robot.Robot) robotResponse {
	return robotResponse{
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
	}
}
