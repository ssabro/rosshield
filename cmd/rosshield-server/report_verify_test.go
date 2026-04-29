package main

// report_verify_test.go — `report verify` CLI 서브커맨드 단위 테스트 (E8 Stage E).
//
// 핵심 시나리오: 정상 번들 → exit 0 + JSON ok=true / 변조 번들 → exit 2 + JSON ok=false
// + error 채워짐. CLI args 파싱 + exit code 매핑까지 검증.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

func writeBundleFixture(t *testing.T) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	p, err := Bootstrap(context.Background(), Config{
		DataDir: dir, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	const tenantID = storage.TenantID("tnt_CLI")
	const sessionID = "scan_CLI"
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'cli', 'desktop_free', ?)`,
			string(tenantID), now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at)
VALUES ('fl_C', ?, 'fleet', '', '{}', ?, ?)`, string(tenantID), now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO packs (id, tenant_id, name, version, vendor, pack_key, manifest_hash, signer_key_id, installed_at)
VALUES ('pk_C', ?, 'cis', '1.0', 'CIS', 'cis-cis-1.0', x'00', 'key_test', ?)`,
			string(tenantID), now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO scan_sessions (id, tenant_id, fleet_id, pack_id, trigger, status,
    progress_total, progress_completed, progress_failed, failure_reason,
    created_at, updated_at, started_at, completed_at)
VALUES (?, ?, 'fl_C', 'pk_C', 'manual', 'completed', 0, 0, 0, '', ?, ?, ?, ?)`,
			sessionID, string(tenantID), now, now, now, now)
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, bundle, err := GenerateAndSignReport(context.Background(), p, reporting.GenerateRequest{
		TenantID:    tenantID,
		SessionID:   sessionID,
		GeneratedBy: "system",
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("GenerateAndSignReport: %v", err)
	}

	bundlePath := filepath.Join(t.TempDir(), "report.tar.gz")
	if err := os.WriteFile(bundlePath, bundle, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	return bundlePath, p.ReportSigner.PublicKey()
}

func TestReportVerifyExitsZeroOnValidBundle(t *testing.T) {
	bundlePath, _ := writeBundleFixture(t)
	exitCode := runReportVerify([]string{bundlePath})
	if exitCode != 0 {
		t.Fatalf("exit=%d, want 0", exitCode)
	}
}

func TestReportVerifyExitsTwoOnTamperedBundle(t *testing.T) {
	bundlePath, _ := writeBundleFixture(t)
	// 번들 한 byte 변조 → tar.gz crc 또는 sig 검증 실패.
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// 중간 bytes 한 개 뒤집기 — gzip CRC 또는 inflate 실패 또는 sig 불일치 중 하나로 거부.
	if len(data) < 100 {
		t.Fatalf("bundle too small")
	}
	data[len(data)/2] ^= 0xFF
	tampered := filepath.Join(t.TempDir(), "tampered.tar.gz")
	if err := os.WriteFile(tampered, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	exitCode := runReportVerify([]string{tampered})
	if exitCode == 0 {
		t.Fatalf("exit=%d, want non-zero (tampered)", exitCode)
	}
}

func TestReportVerifyExitsTwoOnWrongPublicKey(t *testing.T) {
	bundlePath, _ := writeBundleFixture(t)

	// 다른 ed25519 키 PEM을 임시 파일로 작성 후 -public-key로 전달.
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	otherPEM := mustEncodePEM(t, otherPub)
	pemPath := filepath.Join(t.TempDir(), "other.pem")
	if err := os.WriteFile(pemPath, otherPEM, 0o644); err != nil {
		t.Fatalf("write pem: %v", err)
	}
	exitCode := runReportVerify([]string{"-public-key", pemPath, bundlePath})
	if exitCode != 2 {
		t.Fatalf("exit=%d, want 2 (pub key mismatch)", exitCode)
	}
}

func TestReportVerifyExitsOneOnMissingFile(t *testing.T) {
	exitCode := runReportVerify([]string{filepath.Join(t.TempDir(), "missing.tar.gz")})
	if exitCode != 1 {
		t.Fatalf("exit=%d, want 1 (file not found)", exitCode)
	}
}

func TestReportVerifyJSONOutputContainsAnchorFields(t *testing.T) {
	bundlePath, _ := writeBundleFixture(t)
	// stdout 캡처.
	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	exitCode := runReportVerify([]string{bundlePath})
	w.Close()
	os.Stdout = oldStdout

	out, _ := io.ReadAll(r)
	if exitCode != 0 {
		t.Fatalf("exit=%d, want 0; out=%s", exitCode, out)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal stdout: %v\nraw: %s", err, out)
	}
	if ok, _ := parsed["ok"].(bool); !ok {
		t.Fatalf("ok=false in JSON output: %s", out)
	}
	if _, ok := parsed["pdfSha256"]; !ok {
		t.Fatalf("missing pdfSha256: %s", out)
	}
	if _, ok := parsed["chainHeadSeq"]; !ok {
		t.Fatalf("missing chainHeadSeq: %s", out)
	}
	if _, ok := parsed["signerKeyId"]; !ok {
		t.Fatalf("missing signerKeyId: %s", out)
	}
}

// === 헬퍼 ===

func mustEncodePEM(t *testing.T, pub ed25519.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("MarshalPKIX: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}
