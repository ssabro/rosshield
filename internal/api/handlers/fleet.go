package handlers

// fleet.go — Fleet CRUD handlers.
//
// GET /api/v1/fleets               — list (모든 인증 사용자)
// POST /api/v1/fleets              — 생성 (admin)
// PATCH /api/v1/fleets/{fleetId}   — 이름·설명 수정 (admin)
// DELETE /api/v1/fleets/{fleetId}  — soft delete (admin)
//
// fleet 관련 도메인은 robot.Service에 통합되어 있음 (legacy — robot 패키지가 fleet을 함께 소유).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// FleetScanScheduler는 admin이 fleet을 mutation한 직후 cron job을 재등록하는 hook입니다.
//
// bootstrap이 *FleetScanScheduler 구현체를 주입 — handlers.Deps.FleetScanSched.
// nil 가능 (Phase 1 호환·tests).
type FleetScanScheduler interface {
	Reconcile(ctx context.Context, tenantID storage.TenantID, fleetID string)
	Cancel(fleetID string)
}

// fleetResponse는 fleet 응답 항목입니다.
type fleetResponse struct {
	ID          string             `json:"id"`
	TenantID    string             `json:"tenantId"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	RobotCount  int                `json:"robotCount"`
	Policy      fleetPolicyRequest `json:"policy"`
	CreatedAt   string             `json:"createdAt,omitempty"`
	UpdatedAt   string             `json:"updatedAt,omitempty"`
}

// fleetListResponse는 GET /api/v1/fleets 응답 envelope입니다.
type fleetListResponse struct {
	Fleets []fleetResponse `json:"fleets"`
}

// fleetPolicyRequest는 fleet 정책 4 필드 입력입니다.
type fleetPolicyRequest struct {
	DefaultBaselineID  string `json:"defaultBaselineId,omitempty"`
	DefaultLevel       string `json:"defaultLevel,omitempty"`
	DefaultCriticality string `json:"defaultCriticality,omitempty"`
	ScanSchedule       string `json:"scanSchedule,omitempty"`
}

// createFleetRequest는 POST /api/v1/fleets 요청 본문입니다.
type createFleetRequest struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Policy      *fleetPolicyRequest `json:"policy,omitempty"`
}

// updateFleetRequest는 PATCH /api/v1/fleets/{fleetId} 요청 본문입니다.
//
// 모든 필드 옵션. 모두 nil이면 no-op (200, current 반환). policy non-nil이면 통째 교체.
type updateFleetRequest struct {
	Name        *string             `json:"name,omitempty"`
	Description *string             `json:"description,omitempty"`
	Policy      *fleetPolicyRequest `json:"policy,omitempty"`
}

// toDomainPolicy는 wire format을 도메인 FleetPolicy로 변환합니다.
func toDomainPolicy(p fleetPolicyRequest) robot.FleetPolicy {
	return robot.FleetPolicy{
		DefaultBaselineID:  p.DefaultBaselineID,
		DefaultLevel:       robot.Level(p.DefaultLevel),
		DefaultCriticality: robot.Criticality(p.DefaultCriticality),
		ScanSchedule:       p.ScanSchedule,
	}
}

// cronParser는 fleet ScanSchedule 사전 검증용 — cronsched.Scheduler.Schedule이 사용하는
// robfig/cron v3 default parser와 같은 옵션 (5-field standard + descriptors).
//
// 본 dry-run으로 invalid spec을 즉시 400으로 거부 — DB 저장 후 cron 등록 실패의 silent
// degraded 회피.
var cronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// validateCronSpec은 ScanSchedule 비-empty일 때 robfig/cron parse 가능 여부를 검증합니다.
// spec이 비면 nil (수동 스캔 정책 — 유효).
func validateCronSpec(spec string) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	if _, err := cronParser.Parse(spec); err != nil {
		return fmt.Errorf("invalid scanSchedule cron spec: %w", err)
	}
	return nil
}

func toFleetResponse(f robot.Fleet) fleetResponse {
	out := fleetResponse{
		ID:          f.ID,
		TenantID:    string(f.TenantID),
		Name:        f.Name,
		Description: f.Description,
		RobotCount:  f.RobotCount,
		Policy: fleetPolicyRequest{
			DefaultBaselineID:  f.Policy.DefaultBaselineID,
			DefaultLevel:       string(f.Policy.DefaultLevel),
			DefaultCriticality: string(f.Policy.DefaultCriticality),
			ScanSchedule:       f.Policy.ScanSchedule,
		},
	}
	if !f.CreatedAt.IsZero() {
		out.CreatedAt = f.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !f.UpdatedAt.IsZero() {
		out.UpdatedAt = f.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

// ListFleets는 GET /api/v1/fleets 핸들러입니다.
func (h *Handlers) ListFleets(w http.ResponseWriter, r *http.Request) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Robot == nil {
		writeError(w, http.StatusServiceUnavailable, "robot service not configured")
		return
	}

	var fleets []robot.Fleet
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		fs, e := h.deps.Robot.ListFleets(ctx, tx)
		if e != nil {
			return e
		}
		fleets = fs
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "list fleets failed")
		return
	}

	out := fleetListResponse{Fleets: make([]fleetResponse, 0, len(fleets))}
	for _, f := range fleets {
		out.Fleets = append(out.Fleets, toFleetResponse(f))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetFleet는 GET /api/v1/fleets/{fleetId} 핸들러입니다.
//
// tenant scope 단일 fleet 조회 — 모든 인증 사용자 read. 미존재(또는 cross-tenant) → 404.
// 응답에 RobotCount + Policy 4 필드 포함 (ListFleets와 동일 schema).
func (h *Handlers) GetFleet(w http.ResponseWriter, r *http.Request, fleetID string) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if fleetID == "" {
		writeError(w, http.StatusBadRequest, "missing fleetId")
		return
	}
	if h.deps.Robot == nil {
		writeError(w, http.StatusServiceUnavailable, "robot service not configured")
		return
	}

	var f robot.Fleet
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		got, e := h.deps.Robot.GetFleet(ctx, tx, fleetID)
		if e != nil {
			return e
		}
		f = got
		return nil
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "fleet not found")
			return
		}
		writeError(w, errorStatusFor(err), "get fleet failed")
		return
	}
	writeJSON(w, http.StatusOK, toFleetResponse(f))
}

// CreateFleet은 POST /api/v1/fleets 핸들러입니다.
func (h *Handlers) CreateFleet(w http.ResponseWriter, r *http.Request) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Robot == nil {
		writeError(w, http.StatusServiceUnavailable, "robot service not configured")
		return
	}

	var req createFleetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	createReq := robot.CreateFleetRequest{
		Name:        req.Name,
		Description: req.Description,
	}
	if req.Policy != nil {
		if err := validateCronSpec(req.Policy.ScanSchedule); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		createReq.Policy = toDomainPolicy(*req.Policy)
	}
	var created robot.Fleet
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		f, e := h.deps.Robot.CreateFleet(ctx, tx, createReq)
		if e != nil {
			return e
		}
		created = f
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, robot.ErrFleetNameDuplicate):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, robot.ErrFleetEmptyName),
			errors.Is(err, robot.ErrFleetNameTooLong),
			errors.Is(err, robot.ErrFleetInvalidLevel),
			errors.Is(err, robot.ErrFleetInvalidCritical):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, errorStatusFor(err), "create fleet failed")
		}
		return
	}

	if h.deps.FleetScanSched != nil {
		h.deps.FleetScanSched.Reconcile(r.Context(), tenantID, created.ID)
	}
	writeJSON(w, http.StatusCreated, toFleetResponse(created))
}

// UpdateFleet은 PATCH /api/v1/fleets/{fleetId} 핸들러입니다.
func (h *Handlers) UpdateFleet(w http.ResponseWriter, r *http.Request, fleetID string) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if fleetID == "" {
		writeError(w, http.StatusBadRequest, "missing fleetId")
		return
	}
	if h.deps.Robot == nil {
		writeError(w, http.StatusServiceUnavailable, "robot service not configured")
		return
	}

	var req updateFleetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	updateReq := robot.UpdateFleetRequest{
		Name:        req.Name,
		Description: req.Description,
	}
	if req.Policy != nil {
		if err := validateCronSpec(req.Policy.ScanSchedule); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		p := toDomainPolicy(*req.Policy)
		updateReq.Policy = &p
	}
	var updated robot.Fleet
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		f, e := h.deps.Robot.UpdateFleet(ctx, tx, fleetID, updateReq)
		if e != nil {
			return e
		}
		updated = f
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFound):
			writeError(w, http.StatusNotFound, "fleet not found")
		case errors.Is(err, robot.ErrFleetNameDuplicate):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, robot.ErrFleetEmptyName),
			errors.Is(err, robot.ErrFleetNameTooLong),
			errors.Is(err, robot.ErrFleetInvalidLevel),
			errors.Is(err, robot.ErrFleetInvalidCritical):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, errorStatusFor(err), "update fleet failed")
		}
		return
	}

	if h.deps.FleetScanSched != nil {
		h.deps.FleetScanSched.Reconcile(r.Context(), tenantID, updated.ID)
	}
	writeJSON(w, http.StatusOK, toFleetResponse(updated))
}

// DeleteFleet은 DELETE /api/v1/fleets/{fleetId} 핸들러입니다 (soft delete).
func (h *Handlers) DeleteFleet(w http.ResponseWriter, r *http.Request, fleetID string) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if fleetID == "" {
		writeError(w, http.StatusBadRequest, "missing fleetId")
		return
	}
	if h.deps.Robot == nil {
		writeError(w, http.StatusServiceUnavailable, "robot service not configured")
		return
	}

	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		return h.deps.Robot.DeleteFleet(ctx, tx, fleetID)
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "fleet not found")
			return
		}
		writeError(w, errorStatusFor(err), "delete fleet failed")
		return
	}

	if h.deps.FleetScanSched != nil {
		h.deps.FleetScanSched.Cancel(fleetID)
	}
	w.WriteHeader(http.StatusNoContent)
}
