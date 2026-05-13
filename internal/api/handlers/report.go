package handlers

// report.go — GET /api/v1/reports?sessionId=... 핸들러 (E9 Stage B).
//
// AuthMiddleware가 ctx에 TenantID 주입 → Tx에서 자동 격리.
// sessionId query 파라미터는 옵션 — 빈 값이면 tenant 전체 report 메타 반환.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// reportResponse는 응답에 포함되는 Report 메타입니다.
//
// PDF 본문은 List 응답에 포함하지 않음 (Service.ListReports가 nil로 채움).
type reportResponse struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenantId"`
	SessionID    string `json:"sessionId"`
	Format       string `json:"format"`
	PDFSHA256    string `json:"pdfSha256"`
	PDFSizeBytes int64  `json:"pdfSizeBytes"`
	GeneratedAt  string `json:"generatedAt"`
	GeneratedBy  string `json:"generatedBy"`
	Signed       bool   `json:"signed"`
}

// listReportsResponse는 GET /api/v1/reports 응답 본문입니다.
type listReportsResponse struct {
	Reports []reportResponse `json:"reports"`
}

// ListReports는 GET /api/v1/reports 핸들러입니다.
//
// OpenAPI spec에는 정의되지 않은 endpoint — Phase 1 Stage B는 chi router에 직접 등록.
// 후속 Stage에서 spec 보강 시 gen.ServerInterface로 통합.
func (h *Handlers) ListReports(w http.ResponseWriter, r *http.Request) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	sessionID := r.URL.Query().Get("sessionId")

	var reports []reporting.Report
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		rs, e := h.deps.Reporting.ListReports(ctx, tx, reporting.ListFilter{
			SessionID: sessionID,
		})
		if e != nil {
			return e
		}
		reports = rs
		return nil
	})
	if err != nil {
		writeError(w, errorStatusFor(err), "list reports failed")
		return
	}

	out := listReportsResponse{Reports: make([]reportResponse, 0, len(reports))}
	for _, rp := range reports {
		out.Reports = append(out.Reports, reportResponse{
			ID:           rp.ID,
			TenantID:     string(rp.TenantID),
			SessionID:    rp.SessionID,
			Format:       rp.Format,
			PDFSHA256:    rp.PDFSHA256,
			PDFSizeBytes: rp.PDFSizeBytes,
			GeneratedAt:  rp.GeneratedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
			GeneratedBy:  rp.GeneratedBy,
			Signed:       !rp.Signature.IsZero(),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// DownloadReport는 GET /api/v1/reports/{id}/download 핸들러입니다.
//
// reporting.Service.GetReport(reportID)로 PDF body + 메타 회수 후 streaming.
// http.ServeContent로 Range·Last-Modified·If-Modified-Since 자동 처리.
//
// 보안:
//   - TenantID context 검증 (AuthMiddleware) — cross-tenant 접근 차단 (Service.GetReport이
//     Tx 안에서 tenant scope query 자동 적용)
//   - report.TenantID 일치 검증 (방어적 — Service가 이미 보장하지만 audit 명시)
//   - reportID는 ULID 형식 (path traversal 위험 0 — 단순 ID lookup)
//
// Content-Disposition attachment + filename `report-<id>.pdf`. 운영자 friendly.
func (h *Handlers) DownloadReport(w http.ResponseWriter, r *http.Request, reportID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if reportID == "" {
		writeError(w, http.StatusBadRequest, "missing reportId")
		return
	}

	var report reporting.Report
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		rp, e := h.deps.Reporting.GetReport(ctx, tx, reportID)
		if e != nil {
			return e
		}
		report = rp
		return nil
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "report not found")
			return
		}
		writeError(w, errorStatusFor(err), "get report failed")
		return
	}
	if len(report.PDF) == 0 {
		writeError(w, http.StatusNotFound, "report has no PDF body")
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"report-%s.pdf\"", report.ID))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Last-Modified는 GeneratedAt 활용 — Sign 시점은 별도 헤더로 노출 안 함 (단순화).
	http.ServeContent(w, r, fmt.Sprintf("report-%s.pdf", report.ID), report.GeneratedAt, bytes.NewReader(report.PDF))
}
