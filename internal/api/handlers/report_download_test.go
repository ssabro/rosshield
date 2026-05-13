package handlers_test

// report_download_test.go — GET /api/v1/reports/{id}/download 단위 테스트.
//
// handler 직접 호출 (httptest.Server 결선 회피, fixture overhead 0). reporting.Service는
// 가짜 어댑터(stubReportingService)로 PDF body 주입.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/api/handlers"
	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// stubReportingService는 reporting.Service의 download 검증용 최소 구현체.
//
// GetReport만 구현 — 다른 메서드는 panic (DownloadReport handler가 GetReport만 호출).
// 이런 패턴은 다른 handler 단위 테스트의 nullXxxAdapter와 일관.
type stubReportingService struct {
	report reporting.Report
	err    error
}

func (s *stubReportingService) GetReport(ctx context.Context, tx storage.Tx, reportID string) (reporting.Report, error) {
	if s.err != nil {
		return reporting.Report{}, s.err
	}
	if s.report.ID != reportID {
		return reporting.Report{}, storage.ErrNotFound
	}
	return s.report, nil
}

func (s *stubReportingService) Generate(ctx context.Context, tx storage.Tx, req reporting.GenerateRequest) (reporting.Report, error) {
	panic("not used in download tests")
}
func (s *stubReportingService) Sign(ctx context.Context, tx storage.Tx, reportID, signerKeyID string, sigBytes []byte, chainHeadSeq int64, chainHeadHash string, signedAt time.Time) (reporting.Report, error) {
	panic("not used in download tests")
}
func (s *stubReportingService) ListReports(ctx context.Context, tx storage.Tx, filter reporting.ListFilter) ([]reporting.Report, error) {
	panic("not used in download tests")
}
func (s *stubReportingService) GenerateFramework(ctx context.Context, tx storage.Tx, req reporting.GenerateFrameworkRequest) (reporting.FrameworkReport, error) {
	panic("not used in download tests")
}
func (s *stubReportingService) SignFramework(ctx context.Context, tx storage.Tx, reportID, signerKeyID string, sigBytes []byte, chainHeadSeq int64, chainHeadHash string, signedAt time.Time) (reporting.FrameworkReport, error) {
	panic("not used in download tests")
}
func (s *stubReportingService) GetFrameworkReport(ctx context.Context, tx storage.Tx, reportID string) (reporting.FrameworkReport, error) {
	panic("not used in download tests")
}
func (s *stubReportingService) ListFrameworkReports(ctx context.Context, tx storage.Tx, filter reporting.FrameworkListFilter) ([]reporting.FrameworkReport, error) {
	panic("not used in download tests")
}

// noopStorage는 Tx callback을 즉시 nil-tx로 호출 (Storage interface 최소 구현).
type noopStorage struct{}

func (noopStorage) Tx(ctx context.Context, fn func(context.Context, storage.Tx) error) error {
	return fn(ctx, nil) //nolint:nilnil // stub: tx unused by GetReport stub
}
func (noopStorage) Bootstrap(ctx context.Context, fn func(context.Context, storage.Tx) error) error {
	return fn(ctx, nil) //nolint:nilnil
}
func (noopStorage) Migrate(ctx context.Context) error { return nil }
func (noopStorage) Close() error                       { return nil }

func TestDownloadReport_Returns200WithPDF(t *testing.T) {
	t.Parallel()
	const reportID = "rep_test"
	pdfBody := []byte("%PDF-1.7\n... stub body ...")
	stub := &stubReportingService{
		report: reporting.Report{
			ID:           reportID,
			TenantID:     "tn_test",
			PDF:          pdfBody,
			GeneratedAt:  time.Now().UTC(),
			Format:       "pdf",
			PDFSizeBytes: int64(len(pdfBody)),
		},
	}
	h := handlers.New(handlers.Deps{
		Storage:   noopStorage{},
		Reporting: stub,
	})

	req := httptest.NewRequest("GET", "/api/v1/reports/"+reportID+"/download", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), "tn_test"))
	rec := httptest.NewRecorder()

	h.DownloadReport(rec, req, reportID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type=%q, want application/pdf", ct)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, reportID) {
		t.Errorf("Content-Disposition=%q, want attachment + reportID", cd)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options should be nosniff")
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != string(pdfBody) {
		t.Errorf("body mismatch: got %q want %q", string(body), string(pdfBody))
	}
}

func TestDownloadReport_Returns404WhenNotFound(t *testing.T) {
	t.Parallel()
	stub := &stubReportingService{err: storage.ErrNotFound}
	h := handlers.New(handlers.Deps{
		Storage:   noopStorage{},
		Reporting: stub,
	})

	req := httptest.NewRequest("GET", "/api/v1/reports/missing/download", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), "tn_test"))
	rec := httptest.NewRecorder()

	h.DownloadReport(rec, req, "missing")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

func TestDownloadReport_Returns404WhenPDFEmpty(t *testing.T) {
	t.Parallel()
	const reportID = "rep_empty"
	stub := &stubReportingService{
		report: reporting.Report{
			ID:       reportID,
			TenantID: "tn_test",
			PDF:      nil, // 빈 PDF
			Format:   "pdf",
		},
	}
	h := handlers.New(handlers.Deps{
		Storage:   noopStorage{},
		Reporting: stub,
	})

	req := httptest.NewRequest("GET", "/api/v1/reports/"+reportID+"/download", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), "tn_test"))
	rec := httptest.NewRecorder()

	h.DownloadReport(rec, req, reportID)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404 (empty PDF)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no PDF body") {
		t.Errorf("body should mention 'no PDF body': %s", rec.Body.String())
	}
}

func TestDownloadReport_Returns401WithoutTenantContext(t *testing.T) {
	t.Parallel()
	h := handlers.New(handlers.Deps{
		Storage:   noopStorage{},
		Reporting: &stubReportingService{},
	})

	req := httptest.NewRequest("GET", "/api/v1/reports/x/download", nil)
	rec := httptest.NewRecorder()

	h.DownloadReport(rec, req, "x")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s, want 401", rec.Code, rec.Body.String())
	}
}

func TestDownloadReport_Returns400WhenReportIDEmpty(t *testing.T) {
	t.Parallel()
	h := handlers.New(handlers.Deps{
		Storage:   noopStorage{},
		Reporting: &stubReportingService{},
	})

	req := httptest.NewRequest("GET", "/api/v1/reports//download", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), "tn_test"))
	rec := httptest.NewRecorder()

	h.DownloadReport(rec, req, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

// silence lint — errors import는 stub error 비교에 사용
var _ = errors.Is

// === VerifyReport handler 단위 테스트 ===

func TestVerifyReport_Returns400WhenNotSigned(t *testing.T) {
	t.Parallel()
	const reportID = "rep_unsigned"
	stub := &stubReportingService{
		report: reporting.Report{
			ID:        reportID,
			TenantID:  "tn_test",
			PDF:       []byte("pdf"),
			Signature: reporting.ReportSignature{}, // zero
		},
	}
	h := handlers.New(handlers.Deps{
		Storage:      noopStorage{},
		Reporting:    stub,
		ReportSigner: &fakeSigner{},
	})

	req := httptest.NewRequest("POST", "/api/v1/reports/"+reportID+"/verify", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), "tn_test"))
	rec := httptest.NewRecorder()

	h.VerifyReport(rec, req, reportID)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not signed") {
		t.Errorf("body should mention 'not signed': %s", rec.Body.String())
	}
}

func TestVerifyReport_Returns503WhenSignerNotConfigured(t *testing.T) {
	t.Parallel()
	h := handlers.New(handlers.Deps{
		Storage:   noopStorage{},
		Reporting: &stubReportingService{},
		// ReportSigner nil
	})

	req := httptest.NewRequest("POST", "/api/v1/reports/x/verify", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), "tn_test"))
	rec := httptest.NewRecorder()

	h.VerifyReport(rec, req, "x")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s, want 503", rec.Code, rec.Body.String())
	}
}

func TestVerifyReport_Returns404WhenNotFound(t *testing.T) {
	t.Parallel()
	stub := &stubReportingService{err: storage.ErrNotFound}
	h := handlers.New(handlers.Deps{
		Storage:      noopStorage{},
		Reporting:    stub,
		ReportSigner: &fakeSigner{},
	})

	req := httptest.NewRequest("POST", "/api/v1/reports/missing/verify", nil)
	req = req.WithContext(storage.WithTenantID(req.Context(), "tn_test"))
	rec := httptest.NewRecorder()

	h.VerifyReport(rec, req, "missing")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

func TestVerifyReport_Returns401WithoutTenantContext(t *testing.T) {
	t.Parallel()
	h := handlers.New(handlers.Deps{
		Storage:      noopStorage{},
		Reporting:    &stubReportingService{},
		ReportSigner: &fakeSigner{},
	})

	req := httptest.NewRequest("POST", "/api/v1/reports/x/verify", nil)
	rec := httptest.NewRecorder()

	h.VerifyReport(rec, req, "x")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s, want 401", rec.Code, rec.Body.String())
	}
}

// fakeSigner는 PublicKey()만 호출되는 signer.Signer stub. Sign/Verify는 호출 X.
type fakeSigner struct{}

func (fakeSigner) Sign([]byte) ([]byte, string, error) { return nil, "", nil }
func (fakeSigner) Verify([]byte, []byte) error          { return nil }
func (fakeSigner) PublicKey() []byte {
	// 32B Ed25519 PublicKey size — VerifyReport 흐름에서 BuildBundle에 전달.
	return make([]byte, 32)
}
func (fakeSigner) KeyID() string { return "test-key" }
