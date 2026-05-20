package handlers

// audit_gc.go — E32 Stage 4: POST /api/v1/audit/gc/run — manual hot GC trigger.
//
// design: docs/design/notes/audit-chain-rotation-design.md Stage 4.
//
// rotation.HotGC가 archive 완료된 segment 중 hot retention 만료된 entries를 DELETE합니다.
// P9 불변성 트리거는 PG GUC (rosshield.audit_gc_mode='on')로 우회 (마이그레이션 0034).
//
// 권한: ResourceTenantAdmin, ActionAdmin — 다른 destructive 액션 (SSO/Webhook delete) 일관.
//
// query:
//   - ?dry_run=true (default false) — DELETE 미실행, 추정 카운트만 응답.
//
// 응답 (200 OK):
//
//	{
//	  "deletedCount":       <int64>,
//	  "segmentNumbers":     [<n1>, <n2>, ...],
//	  "oldestKeptEntrySeq": <int64>,
//	  "dryRun":             <bool>
//	}
//
// HotGC 미주입 (deps.HotGC == nil)이면 503 Service Unavailable.

import (
	"context"
	"net/http"
	"strconv"

	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// auditGCResponse는 POST /api/v1/audit/gc/run 응답 본문입니다.
type auditGCResponse struct {
	DeletedCount       int64   `json:"deletedCount"`
	SegmentNumbers     []int64 `json:"segmentNumbers"`
	OldestKeptEntrySeq int64   `json:"oldestKeptEntrySeq"`
	DryRun             bool    `json:"dryRun"`
}

// RunAuditGC는 POST /api/v1/audit/gc/run 핸들러입니다.
//
// 본 endpoint 는 AuthMiddleware 통과 + RequirePermission(tenant_admin, admin) 게이트 적용.
// tenant scope 는 ctx 의 TenantID 기준 (cross-tenant DELETE 불가).
func (h *Handlers) RunAuditGC(w http.ResponseWriter, r *http.Request) {
	if h.deps.HotGC == nil {
		writeError(w, http.StatusServiceUnavailable, "audit hot GC not configured")
		return
	}
	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	dryRun := false
	if v := r.URL.Query().Get("dry_run"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "dry_run must be boolean (true/false)")
			return
		}
		dryRun = parsed
	}

	var result *rotation.HotGCResult
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		r, e := h.deps.HotGC.Run(ctx, tx, tenantID, dryRun)
		if e != nil {
			return e
		}
		result = r
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "audit hot GC failed: "+err.Error())
		return
	}

	segs := result.SegmentsProcessed
	if segs == nil {
		segs = []int64{}
	}
	writeJSON(w, http.StatusOK, auditGCResponse{
		DeletedCount:       result.DeletedCount,
		SegmentNumbers:     segs,
		OldestKeptEntrySeq: result.OldestKeptEntrySeq,
		DryRun:             result.DryRun,
	})
}
