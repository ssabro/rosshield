package handlers

// fleet.go — GET /api/v1/fleets 핸들러.
//
// tenant scope 살아있는 fleets를 name ASC로 반환. 모든 인증 사용자 read.
// scans 페이지 fleet dropdown + 다른 페이지 fleet 조회 동시 활용.

import (
	"context"
	"net/http"
	"time"

	"github.com/ssabro/rosshield/internal/domain/fleet"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// fleetResponse는 GET /api/v1/fleets 응답 항목입니다.
type fleetResponse struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

// fleetListResponse는 GET /api/v1/fleets 응답 envelope입니다.
type fleetListResponse struct {
	Fleets []fleetResponse `json:"fleets"`
}

// ListFleets는 GET /api/v1/fleets 핸들러입니다.
func (h *Handlers) ListFleets(w http.ResponseWriter, r *http.Request) {
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Fleet == nil {
		writeError(w, http.StatusServiceUnavailable, "fleet service not configured")
		return
	}

	var fleets []fleet.Fleet
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		fs, e := h.deps.Fleet.ListFleets(ctx, tx)
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
		fr := fleetResponse{
			ID:          f.ID,
			TenantID:    string(f.TenantID),
			Name:        f.Name,
			Description: f.Description,
		}
		if !f.CreatedAt.IsZero() {
			fr.CreatedAt = f.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		if !f.UpdatedAt.IsZero() {
			fr.UpdatedAt = f.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
		out.Fleets = append(out.Fleets, fr)
	}
	writeJSON(w, http.StatusOK, out)
}
