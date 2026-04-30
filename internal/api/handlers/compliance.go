package handlers

// compliance.go — Compliance 도메인 HTTP 표면 (E17 Phase 2).
//
// 엔드포인트 4종:
//
//	GET  /api/v1/compliance/profiles                         → ListComplianceProfiles
//	POST /api/v1/compliance/profiles                         → CreateComplianceProfile
//	GET  /api/v1/compliance/profiles/{profileId}/snapshots   → ListComplianceSnapshots
//	POST /api/v1/compliance/profiles/{profileId}/snapshots   → GenerateComplianceSnapshot
//
// 도메인 결합 (P5): handlers는 compliance.Service interface만 호출. ScanReader/AuditReader는
// bootstrap이 결선한 어댑터를 거쳐 동작.

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/ssabro/rosshield/internal/api/gen"
	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

type profileResponse struct {
	ID               string `json:"id"`
	TenantID         string `json:"tenantId"`
	Framework        string `json:"framework"`
	FrameworkVersion string `json:"frameworkVersion"`
	Enabled          bool   `json:"enabled"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

type listProfilesResponse struct {
	Profiles []profileResponse `json:"profiles"`
}

type controlStatusResponse struct {
	ControlID string `json:"controlId"`
	Status    string `json:"status"`
	PassCount int    `json:"passCount"`
	FailCount int    `json:"failCount"`
	Notes     string `json:"notes,omitempty"`
}

type snapshotResponse struct {
	ID                 string                  `json:"id"`
	TenantID           string                  `json:"tenantId"`
	ProfileID          string                  `json:"profileId"`
	SessionID          string                  `json:"sessionId,omitempty"`
	OverallScore       float64                 `json:"overallScore"`
	PassCount          int                     `json:"passCount"`
	FailCount          int                     `json:"failCount"`
	PartialCount       int                     `json:"partialCount"`
	NotApplicableCount int                     `json:"notApplicableCount"`
	UnmappedCount      int                     `json:"unmappedCount"`
	ChainHeadSeq       int64                   `json:"chainHeadSeq"`
	ChainHeadHash      string                  `json:"chainHeadHash"`
	Statuses           []controlStatusResponse `json:"statuses,omitempty"`
	CreatedAt          string                  `json:"createdAt"`
}

type listSnapshotsResponse struct {
	Snapshots []snapshotResponse `json:"snapshots"`
}

// ListComplianceProfiles는 GET /api/v1/compliance/profiles 핸들러입니다.
func (h *Handlers) ListComplianceProfiles(w http.ResponseWriter, r *http.Request) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	var profiles []compliance.ComplianceProfile
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Compliance.ListProfiles(ctx, tx)
		if e != nil {
			return e
		}
		profiles = out
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "list profiles failed")
		return
	}

	out := listProfilesResponse{Profiles: make([]profileResponse, 0, len(profiles))}
	for _, p := range profiles {
		out.Profiles = append(out.Profiles, mapProfile(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateComplianceProfile는 POST /api/v1/compliance/profiles 핸들러입니다.
//
// 중복(같은 framework) 시 409 Conflict, 버전 불일치 시 400 Bad Request.
func (h *Handlers) CreateComplianceProfile(w http.ResponseWriter, r *http.Request) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	var body gen.CreateComplianceProfileJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	req := compliance.CreateProfileRequest{
		Framework:        compliance.Framework(body.Framework),
		FrameworkVersion: body.FrameworkVersion,
		Enabled:          enabled,
	}

	var created compliance.ComplianceProfile
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Compliance.CreateProfile(ctx, tx, req)
		if e != nil {
			return e
		}
		created = out
		return nil
	})
	if err != nil {
		writeError(w, complianceErrorStatus(err), "create profile failed")
		return
	}
	writeJSON(w, http.StatusCreated, mapProfile(created))
}

// ListComplianceSnapshots는 GET /api/v1/compliance/profiles/{profileId}/snapshots 핸들러입니다.
func (h *Handlers) ListComplianceSnapshots(w http.ResponseWriter, r *http.Request, profileID string, params gen.ListComplianceSnapshotsParams) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	limit := 0
	if params.Limit != nil {
		limit = *params.Limit
	}

	var snaps []compliance.FrameworkSnapshot
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Compliance.ListSnapshots(ctx, tx, profileID, limit)
		if e != nil {
			return e
		}
		snaps = out
		return nil
	})
	if err != nil {
		writeError(w, complianceErrorStatus(err), "list snapshots failed")
		return
	}

	out := listSnapshotsResponse{Snapshots: make([]snapshotResponse, 0, len(snaps))}
	for _, s := range snaps {
		out.Snapshots = append(out.Snapshots, mapSnapshot(s))
	}
	writeJSON(w, http.StatusOK, out)
}

// GenerateComplianceSnapshot는 POST /api/v1/compliance/profiles/{profileId}/snapshots 핸들러입니다.
//
// sessionId는 어떤 ScanSession 결과로 평가할지 — 옵션 anchor로 framework_snapshots에 저장.
func (h *Handlers) GenerateComplianceSnapshot(w http.ResponseWriter, r *http.Request, profileID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	var body gen.GenerateComplianceSnapshotJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.SessionId == "" {
		writeError(w, http.StatusBadRequest, "sessionId is required")
		return
	}

	var snap compliance.FrameworkSnapshot
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Compliance.GenerateSnapshot(ctx, tx, profileID, body.SessionId)
		if e != nil {
			return e
		}
		snap = out
		return nil
	})
	if err != nil {
		writeError(w, complianceErrorStatus(err), "generate snapshot failed")
		return
	}
	writeJSON(w, http.StatusCreated, mapSnapshot(snap))
}

func mapProfile(p compliance.ComplianceProfile) profileResponse {
	return profileResponse{
		ID:               p.ID,
		TenantID:         string(p.TenantID),
		Framework:        string(p.Framework),
		FrameworkVersion: p.FrameworkVersion,
		Enabled:          p.Enabled,
		CreatedAt:        p.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
		UpdatedAt:        p.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
	}
}

func mapSnapshot(s compliance.FrameworkSnapshot) snapshotResponse {
	resp := snapshotResponse{
		ID:                 s.ID,
		TenantID:           string(s.TenantID),
		ProfileID:          s.ProfileID,
		SessionID:          s.SessionID,
		OverallScore:       s.OverallScore,
		PassCount:          s.PassCount,
		FailCount:          s.FailCount,
		PartialCount:       s.PartialCount,
		NotApplicableCount: s.NotApplicableCount,
		UnmappedCount:      s.UnmappedCount,
		ChainHeadSeq:       s.ChainHeadSeq,
		ChainHeadHash:      s.ChainHeadHash,
		Statuses:           make([]controlStatusResponse, 0, len(s.Statuses)),
		CreatedAt:          s.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
	}
	for _, st := range s.Statuses {
		resp.Statuses = append(resp.Statuses, controlStatusResponse{
			ControlID: st.ControlID,
			Status:    string(st.Status),
			PassCount: st.PassCount,
			FailCount: st.FailCount,
			Notes:     st.Notes,
		})
	}
	return resp
}
