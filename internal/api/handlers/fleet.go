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
	"net/http"
	"time"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// fleetResponse는 fleet 응답 항목입니다.
type fleetResponse struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	RobotCount  int    `json:"robotCount"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

// fleetListResponse는 GET /api/v1/fleets 응답 envelope입니다.
type fleetListResponse struct {
	Fleets []fleetResponse `json:"fleets"`
}

// createFleetRequest는 POST /api/v1/fleets 요청 본문입니다.
type createFleetRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// updateFleetRequest는 PATCH /api/v1/fleets/{fleetId} 요청 본문입니다.
//
// 두 필드 모두 옵션. 둘 다 nil이면 no-op (200, current 반환).
type updateFleetRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

func toFleetResponse(f robot.Fleet) fleetResponse {
	out := fleetResponse{
		ID:          f.ID,
		TenantID:    string(f.TenantID),
		Name:        f.Name,
		Description: f.Description,
		RobotCount:  f.RobotCount,
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

	var created robot.Fleet
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		f, e := h.deps.Robot.CreateFleet(ctx, tx, robot.CreateFleetRequest{
			Name:        req.Name,
			Description: req.Description,
		})
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

	var updated robot.Fleet
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		f, e := h.deps.Robot.UpdateFleet(ctx, tx, fleetID, robot.UpdateFleetRequest{
			Name:        req.Name,
			Description: req.Description,
		})
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
			errors.Is(err, robot.ErrFleetNameTooLong):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, errorStatusFor(err), "update fleet failed")
		}
		return
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

	w.WriteHeader(http.StatusNoContent)
}
