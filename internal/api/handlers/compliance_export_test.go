package handlers

// compliance_export_test.go — Phase 11.B-5 audit log export wizard 단위 테스트.
//
// 검증:
//   - 의존성 미주입 → 503 (옵트인 게이트)
//   - tenant context 부재 → 401
//   - format 검증 (v1/v2 only, 그 외 400)
//   - happy path → 200 + Content-Type application/gzip + audit emit
//   - audit.compliance.export event 가 동일 Tx 에 emit 되어 entry seq 가 응답 헤더에 포함
//   - v1 format fallback

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

const exportTestTenant storage.TenantID = "system"

func TestExportComplianceBundle_NoDeps_503(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Storage: openTestStorage(t),
		Clock:   clock.System(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/export", bytes.NewReader([]byte(`{"format":"v2"}`)))
	req = req.WithContext(storage.WithTenantID(req.Context(), exportTestTenant))
	rec := httptest.NewRecorder()

	h.ExportComplianceBundle(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestExportComplianceBundle_NoTenant_401(t *testing.T) {
	t.Parallel()

	store := openTestStorage(t)
	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})
	initial, _ := soft.New()
	swap := signer.NewSwappableSigner(initial, 1)

	h := New(Deps{
		Storage:        store,
		Clock:          clk,
		Audit:          auditSvc,
		AuditExporter:  auditSvc,
		AuditChainKeys: auditrepo.NewKeyEpochRepo(),
		AuditSigner:    swap,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/export", bytes.NewReader([]byte(`{}`)))
	// tenant context 미주입
	rec := httptest.NewRecorder()

	h.ExportComplianceBundle(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestExportComplianceBundle_InvalidFormat_400(t *testing.T) {
	t.Parallel()

	store := openTestStorage(t)
	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})
	initial, _ := soft.New()
	swap := signer.NewSwappableSigner(initial, 1)

	h := New(Deps{
		Storage:        store,
		Clock:          clk,
		Audit:          auditSvc,
		AuditExporter:  auditSvc,
		AuditChainKeys: auditrepo.NewKeyEpochRepo(),
		AuditSigner:    swap,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/export", bytes.NewReader([]byte(`{"format":"v9"}`)))
	req = req.WithContext(storage.WithTenantID(req.Context(), exportTestTenant))
	rec := httptest.NewRecorder()

	h.ExportComplianceBundle(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestExportComplianceBundle_HappyPath_V2_200(t *testing.T) {
	t.Parallel()

	store := openTestStorage(t)
	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})
	initial, _ := soft.New()
	swap := signer.NewSwappableSigner(initial, 1)

	// seed 3 entries (export 대상 + audit emit 후 4번째 entry 가 audit.compliance.export).
	seedAuditEntries(t, store, auditSvc, exportTestTenant, 3)

	h := New(Deps{
		Storage:        store,
		Clock:          clk,
		Audit:          auditSvc,
		AuditExporter:  auditSvc,
		AuditChainKeys: auditrepo.NewKeyEpochRepo(),
		AuditSigner:    swap,
	})

	body := []byte(`{"fromSeq":1,"toSeq":3,"format":"v2"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/export", bytes.NewReader(body))
	ctx := withClaims(req.Context(), tenant.AccessClaims{
		Subject:  "us_auditor",
		TenantID: exportTestTenant,
	})
	ctx = storage.WithTenantID(ctx, exportTestTenant)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ExportComplianceBundle(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/gzip" {
		t.Errorf("Content-Type = %q, want application/gzip", got)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment;") {
		t.Errorf("Content-Disposition = %q, want attachment", cd)
	}
	if rec.Body.Len() == 0 {
		t.Error("body is empty")
	}
	if entrySeq := rec.Header().Get("X-Rosshield-Audit-Entry-Seq"); entrySeq == "" {
		t.Error("X-Rosshield-Audit-Entry-Seq missing")
	}
	if got := rec.Header().Get("X-Rosshield-Export-Format"); got != "v2" {
		t.Errorf("X-Rosshield-Export-Format = %q, want v2", got)
	}

	// audit emit 확인 — Tx 종료 후 head.Seq 는 4 (seed 3 + export emit 1) 이어야.
	ctx2 := storage.WithTenantID(context.Background(), exportTestTenant)
	var headSeq int64
	if err := store.Tx(ctx2, func(c context.Context, tx storage.Tx) error {
		head, e := auditSvc.Head(c, tx, exportTestTenant)
		if e != nil {
			return e
		}
		headSeq = head.Seq
		return nil
	}); err != nil {
		t.Fatalf("read head: %v", err)
	}
	if headSeq != 4 {
		t.Errorf("audit head seq = %d, want 4 (3 seed + 1 export emit)", headSeq)
	}
}

func TestExportComplianceBundle_V1Format_200(t *testing.T) {
	t.Parallel()

	store := openTestStorage(t)
	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})
	initial, _ := soft.New()
	swap := signer.NewSwappableSigner(initial, 1)
	seedAuditEntries(t, store, auditSvc, exportTestTenant, 1)

	h := New(Deps{
		Storage:        store,
		Clock:          clk,
		Audit:          auditSvc,
		AuditExporter:  auditSvc,
		AuditChainKeys: auditrepo.NewKeyEpochRepo(),
		AuditSigner:    swap,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/export", bytes.NewReader([]byte(`{"format":"v1"}`)))
	req = req.WithContext(storage.WithTenantID(req.Context(), exportTestTenant))
	rec := httptest.NewRecorder()

	h.ExportComplianceBundle(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Rosshield-Export-Format"); got != "v1" {
		t.Errorf("X-Rosshield-Export-Format = %q, want v1", got)
	}
}

func TestExportComplianceBundle_EmptyBody_DefaultsV2(t *testing.T) {
	t.Parallel()

	store := openTestStorage(t)
	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})
	initial, _ := soft.New()
	swap := signer.NewSwappableSigner(initial, 1)
	seedAuditEntries(t, store, auditSvc, exportTestTenant, 1)

	h := New(Deps{
		Storage:        store,
		Clock:          clk,
		Audit:          auditSvc,
		AuditExporter:  auditSvc,
		AuditChainKeys: auditrepo.NewKeyEpochRepo(),
		AuditSigner:    swap,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/export", http.NoBody)
	req = req.WithContext(storage.WithTenantID(req.Context(), exportTestTenant))
	rec := httptest.NewRecorder()

	h.ExportComplianceBundle(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Rosshield-Export-Format"); got != "v2" {
		t.Errorf("default format = %q, want v2", got)
	}
}
