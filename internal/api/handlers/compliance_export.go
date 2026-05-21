package handlers

// compliance_export.go — Phase 11.B-5 audit log export wizard backend.
//
// POST /api/v1/compliance/export — 외부 감사인이 audit chain bundle 을 download 할 수 있는
// endpoint. design doc `docs/design/notes/soc2-readiness-design.md` §7.5 (Stage 11.B-5).
//
// 권한: ResourceAudit.ActionExport (RBAC fine-grained 매트릭스 — admin + auditor 통과).
// 본 endpoint 는 read + export 외 mutation 권한 없음 (auditor role 안전 위임 대상).
//
// 요청 body:
//
//	{
//	  "fromSeq":  <int64>,      // 0 또는 미지정이면 1 부터.
//	  "toSeq":    <int64>,      // 0 또는 미지정이면 head.Seq 까지.
//	  "format":   "v2" | "v1"   // 기본 "v2" (chainKeyEpochs 포함). "v1" 호환.
//	}
//
// 응답: 200 OK + Content-Type "application/gzip" + Content-Disposition attachment.
// body 는 NDJSON+gzip stream (Repo.ExportV2 결과).
//
// audit emit: `audit.compliance.export` event (actor + tenant + range + format).
//
// 503 조건: AuditExporter / AuditChainKeys / AuditSigner 미주입 (옵트인 게이트).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// complianceExportRequest 는 POST /api/v1/compliance/export 요청 본문입니다.
type complianceExportRequest struct {
	FromSeq int64  `json:"fromSeq"`
	ToSeq   int64  `json:"toSeq"`
	Format  string `json:"format"`
}

// ExportComplianceBundle 은 POST /api/v1/compliance/export 핸들러입니다 (Phase 11.B-5).
//
// RBAC: caller 는 `audit.export` 권한 보유 (admin + auditor — permission_matrix.go §3.3).
// body 파싱 → ExportV2 → audit emit → gzip stream 응답.
//
// AuditExporter / AuditChainKeys / AuditSigner 중 하나라도 nil 이면 503.
func (h *Handlers) ExportComplianceBundle(w http.ResponseWriter, r *http.Request) {
	if h.deps.AuditExporter == nil || h.deps.AuditChainKeys == nil || h.deps.AuditSigner == nil {
		writeError(w, http.StatusServiceUnavailable, "audit log export not configured")
		return
	}

	tenantID := storage.TenantIDFromContext(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	var req complianceExportRequest
	if r.ContentLength > 0 || r.Body != http.NoBody {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			if err.Error() != "EOF" {
				writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}
		}
	}

	format := req.Format
	if format == "" {
		format = "v2"
	}
	if format != "v1" && format != "v2" {
		writeError(w, http.StatusBadRequest, "format must be 'v1' or 'v2'")
		return
	}

	claims, _ := claimsFromContext(r.Context())
	actorID := claims.Subject
	if actorID == "" {
		actorID = "system"
	}

	// keyRepo 는 v2 면 dependency, v1 이면 nil 전달로 byte-identical fallback.
	var keyRepoForExport audit.ChainKeyRepository
	if format == "v2" {
		keyRepoForExport = h.deps.AuditChainKeys
	}

	// 1) Tx 내부에서 bundle stream 생성 + audit emit.
	var bundleBytes []byte
	var emittedSeq int64
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		rc, e := h.deps.AuditExporter.ExportV2(ctx, tx, tenantID, req.FromSeq, req.ToSeq, h.deps.AuditSigner, keyRepoForExport)
		if e != nil {
			return fmt.Errorf("export: %w", e)
		}
		defer func() { _ = rc.Close() }()

		buf, e := io.ReadAll(rc)
		if e != nil {
			return fmt.Errorf("read bundle: %w", e)
		}
		bundleBytes = buf

		payload := fmt.Sprintf(
			`{"fromSeq":%d,"toSeq":%d,"format":%q,"bytes":%d}`,
			req.FromSeq, req.ToSeq, format, len(bundleBytes))

		entry, e := h.deps.Audit.Append(ctx, tx, audit.AppendRequest{
			TenantID: tenantID,
			Actor:    audit.Actor{Type: audit.ActorUser, ID: actorID},
			Action:   "audit.compliance.export",
			Target:   audit.Target{Type: "audit_chain", ID: string(tenantID)},
			Payload:  []byte(payload),
			Outcome:  audit.OutcomeSuccess,
		})
		if e != nil {
			return fmt.Errorf("audit emit: %w", e)
		}
		emittedSeq = entry.Seq
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "compliance export failed: "+err.Error())
		return
	}

	filename := fmt.Sprintf("audit-bundle-%s-%s.ndjson.gz",
		string(tenantID), time.Now().UTC().Format("20060102T150405Z"))

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(bundleBytes)))
	w.Header().Set("X-Rosshield-Audit-Entry-Seq", strconv.FormatInt(emittedSeq, 10))
	w.Header().Set("X-Rosshield-Export-Format", format)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bundleBytes)
}
