package main

// framework_integration_test.go — E18-C Framework 리포트 end-to-end 검증.
//
// 시나리오: bootstrap → tenant + admin seed → compliance profile 생성 →
//   Snapshot 생성(scan results 없음 → all unmapped) → GenerateAndSignFrameworkReport →
//   서명 + audit anchor + PDF sha256 일관성 검증.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

func TestFrameworkReportFullFlowEndToEnd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	// 1) Tenant + admin user 시드 (tenant.Service.Create) — admin user는 audit Actor에 사용 안 됨.
	const adminEmail = "admin@fwtest.local"
	var tenantID storage.TenantID
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		res, err := p.Tenant.Create(ctx, tx, tenant.CreateRequest{
			Name:             "FW Test Tenant",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       adminEmail,
			AdminPassword:    "testpassword123",
			AdminDisplayName: "FW Admin",
		})
		if err != nil {
			return err
		}
		tenantID = res.Tenant.ID
		return nil
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// 2) Compliance profile + snapshot 생성 (실 도메인 호출, sessionID 임의).
	tCtx := storage.WithTenantID(context.Background(), tenantID)
	var profileID, snapshotID string
	if err := p.Storage.Tx(tCtx, func(ctx context.Context, tx storage.Tx) error {
		profile, err := p.Compliance.CreateProfile(ctx, tx, compliance.CreateProfileRequest{
			Framework:        compliance.FrameworkISMSP,
			FrameworkVersion: "2024",
			Enabled:          true,
		})
		if err != nil {
			return err
		}
		profileID = profile.ID

		snap, err := p.Compliance.GenerateSnapshot(ctx, tx, profileID, "scan_FAKE")
		if err != nil {
			return err
		}
		snapshotID = snap.ID
		return nil
	}); err != nil {
		t.Fatalf("seed compliance: %v", err)
	}

	// 3) GenerateAndSignFrameworkReport — 어댑터 결선 + 서명 흐름 일괄.
	signed, err := GenerateAndSignFrameworkReport(context.Background(), p, reporting.GenerateFrameworkRequest{
		TenantID:    tenantID,
		ProfileID:   profileID,
		SnapshotID:  snapshotID,
		GeneratedBy: "test",
	})
	if err != nil {
		t.Fatalf("GenerateAndSignFrameworkReport: %v", err)
	}

	// 4) 검증: 서명 placeholder가 갱신됐고, sha256가 일치.
	if !strings.HasPrefix(signed.ID, "frep_") {
		t.Errorf("ID = %q, want frep_ prefix", signed.ID)
	}
	if signed.Signature.IsZero() {
		t.Errorf("Signature should not be zero after Sign")
	}
	if signed.Signature.SignerKeyID == "" {
		t.Errorf("SignerKeyID empty")
	}
	if signed.PDFSizeBytes == 0 {
		t.Errorf("PDFSizeBytes 0")
	}
	if !strings.HasPrefix(string(signed.PDF[:8]), "%PDF-") {
		t.Errorf("PDF body does not start with %%PDF-: %q", signed.PDF[:8])
	}

	// 5) sha256 cross-check — DB에 저장된 sha와 PDF 본문 sha 일치.
	hash := sha256.Sum256(signed.PDF)
	wantSHA := hex.EncodeToString(hash[:])
	if signed.PDFSHA256 != wantSHA {
		t.Errorf("PDFSHA256 mismatch: got %s, want %s", signed.PDFSHA256, wantSHA)
	}

	// 6) GetFrameworkReport 라운드트립 — 같은 PDF 본문 + 갱신된 signature.
	var fetched reporting.FrameworkReport
	if err := p.Storage.Tx(tCtx, func(ctx context.Context, tx storage.Tx) error {
		out, err := p.Reporting.GetFrameworkReport(ctx, tx, signed.ID)
		fetched = out
		return err
	}); err != nil {
		t.Fatalf("GetFrameworkReport: %v", err)
	}
	if fetched.PDFSHA256 != wantSHA {
		t.Errorf("fetched PDFSHA256 mismatch")
	}
	if fetched.Signature.IsZero() {
		t.Errorf("fetched Signature is zero")
	}
	if fetched.Signature.ChainHeadSeq <= 0 {
		t.Errorf("fetched ChainHeadSeq = %d, want > 0 (audit anchor)", fetched.Signature.ChainHeadSeq)
	}
}
