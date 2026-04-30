package main

// framework_verify_test.go — `report verify-framework` CLI 서브커맨드 단위 테스트.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// writeFrameworkBundleFixture는 실 도메인 흐름으로 framework 번들 1개를 생성합니다.
// 반환 (bundle 경로).
func writeFrameworkBundleFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p, err := Bootstrap(context.Background(), Config{
		DataDir: dir, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	// tenant + admin.
	var tenantID storage.TenantID
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		res, err := p.Tenant.Create(ctx, tx, tenant.CreateRequest{
			Name:             "FW Verify Test",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       "admin@fwv.local",
			AdminPassword:    "longpassword123",
			AdminDisplayName: "Admin",
		})
		if err != nil {
			return err
		}
		tenantID = res.Tenant.ID
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// compliance profile + snapshot.
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
		t.Fatalf("compliance seed: %v", err)
	}

	_, bundle, err := GenerateAndSignFrameworkReport(context.Background(), p, reporting.GenerateFrameworkRequest{
		TenantID:    tenantID,
		ProfileID:   profileID,
		SnapshotID:  snapshotID,
		GeneratedBy: "test",
	})
	if err != nil {
		t.Fatalf("GenerateAndSignFrameworkReport: %v", err)
	}

	bundlePath := filepath.Join(t.TempDir(), "framework.tar.gz")
	if err := os.WriteFile(bundlePath, bundle, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	return bundlePath
}

func TestFrameworkVerifyExitsZeroOnValidBundle(t *testing.T) {
	bundlePath := writeFrameworkBundleFixture(t)
	exitCode := runFrameworkVerify([]string{bundlePath})
	if exitCode != 0 {
		t.Errorf("exit=%d, want 0", exitCode)
	}
}

func TestFrameworkVerifyExitsTwoOnTamperedBundle(t *testing.T) {
	bundlePath := writeFrameworkBundleFixture(t)
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) < 100 {
		t.Fatalf("bundle too small")
	}
	data[len(data)/2] ^= 0xFF
	tampered := filepath.Join(t.TempDir(), "tampered.tar.gz")
	if err := os.WriteFile(tampered, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	exitCode := runFrameworkVerify([]string{tampered})
	if exitCode == 0 {
		t.Errorf("exit=%d, want non-zero (tampered)", exitCode)
	}
}

func TestFrameworkVerifyExitsOneOnMissingFile(t *testing.T) {
	exitCode := runFrameworkVerify([]string{filepath.Join(t.TempDir(), "missing.tar.gz")})
	if exitCode != 1 {
		t.Errorf("exit=%d, want 1", exitCode)
	}
}

func TestFrameworkVerifyJSONOutputContainsAnchorFields(t *testing.T) {
	bundlePath := writeFrameworkBundleFixture(t)
	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	exitCode := runFrameworkVerify([]string{bundlePath})
	_ = w.Close()
	os.Stdout = oldStdout

	out, _ := io.ReadAll(r)
	if exitCode != 0 {
		t.Fatalf("exit=%d, want 0; out=%s", exitCode, out)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if ok, _ := parsed["ok"].(bool); !ok {
		t.Fatalf("ok=false in JSON: %s", out)
	}
	for _, key := range []string{"pdfSha256", "framework", "frameworkVersion", "profileId", "snapshotId", "chainHeadSeq", "signerKeyId"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("missing %s in JSON: %s", key, out)
		}
	}
	if fw, _ := parsed["framework"].(string); fw != "isms-p" {
		t.Errorf("framework = %q, want isms-p", fw)
	}
}

func TestReportSubcommandRoutesVerifyFramework(t *testing.T) {
	bundlePath := writeFrameworkBundleFixture(t)
	exitCode := reportSubcommand([]string{"verify-framework", bundlePath})
	if exitCode != 0 {
		t.Errorf("exit=%d, want 0 (verify-framework dispatch)", exitCode)
	}
}
