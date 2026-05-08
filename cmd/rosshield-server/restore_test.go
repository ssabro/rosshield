package main

// restore_test.go — `rosshield-server restore` 서브커맨드 단위 테스트 (E28 T1/T3).
//
// 시나리오:
//
//	T1 round-trip: backup → 새 디렉터리에 restore → 도메인 read 동등 (tenant·user count).
//	T3 빈 디렉터리 가드: 기존 파일 있으면 거부 (--force 없이는 에러).
//	  T3-b --force: 기존 파일 있어도 진행.
//	  추가: 손상된 tar 파일은 거부.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// restoreRun은 stdout/stderr·exit code 캡처 결과입니다.
type restoreRun struct {
	exit   int
	stdout string
	stderr string
}

// runRestoreCapture는 stdout/stderr·exit code를 캡처합니다.
func runRestoreCapture(t *testing.T, args []string) restoreRun {
	t.Helper()
	oldStdout, oldStderr := os.Stdout, os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	exit := restoreSubcommand(args)

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var bufOut, bufErr bytes.Buffer
	_, _ = io.Copy(&bufOut, rOut)
	_, _ = io.Copy(&bufErr, rErr)

	return restoreRun{exit: exit, stdout: bufOut.String(), stderr: bufErr.String()}
}

func TestBackupRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	tenantID, _ := seedTenantForBackup(t, srcDir, "round-trip@example.com")

	outPath := filepath.Join(t.TempDir(), "backup.tar.gz")
	bRes := runBackupCapture(t, []string{"--output", outPath, "--data-dir", srcDir})
	if bRes.exit != 0 {
		t.Fatalf("backup exit=%d, want 0; stderr=%s", bRes.exit, bRes.stderr)
	}

	// 새 빈 디렉터리에 restore.
	dstDir := t.TempDir()
	rRes := runRestoreCapture(t, []string{"--input", outPath, "--data-dir", dstDir})
	if rRes.exit != 0 {
		t.Fatalf("restore exit=%d, want 0; stderr=%s", rRes.exit, rRes.stderr)
	}

	// stdout JSON 검증.
	var rOut map[string]any
	if err := json.Unmarshal([]byte(rRes.stdout), &rOut); err != nil {
		t.Fatalf("unmarshal restore stdout: %v\nraw: %s", err, rRes.stdout)
	}
	for _, k := range []string{"restoredFrom", "dataDir", "sizeBytes"} {
		if _, ok := rOut[k]; !ok {
			t.Errorf("missing JSON key %q in %s", k, rRes.stdout)
		}
	}

	// data.db + keys/ 가 dst에 존재하는지.
	if _, err := os.Stat(filepath.Join(dstDir, "data.db")); err != nil {
		t.Errorf("restored data.db missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "keys", "platform.ed25519")); err != nil {
		t.Errorf("restored keys/platform.ed25519 missing: %v", err)
	}

	// 도메인 read 동등성 — tenant + admin user.
	srcTenants, srcUsers := domainCounts(t, srcDir, tenantID)
	dstTenants, dstUsers := domainCounts(t, dstDir, tenantID)
	if srcTenants != dstTenants {
		t.Errorf("tenant count mismatch: src=%d, dst=%d", srcTenants, dstTenants)
	}
	if srcUsers != dstUsers {
		t.Errorf("user count mismatch: src=%d, dst=%d", srcUsers, dstUsers)
	}
}

func TestRestoreRefusesNonEmptyDir(t *testing.T) {
	srcDir := t.TempDir()
	_, _ = seedTenantForBackup(t, srcDir, "guard@example.com")

	outPath := filepath.Join(t.TempDir(), "backup.tar.gz")
	bRes := runBackupCapture(t, []string{"--output", outPath, "--data-dir", srcDir})
	if bRes.exit != 0 {
		t.Fatalf("backup exit=%d, want 0; stderr=%s", bRes.exit, bRes.stderr)
	}

	// 빈 디렉터리가 아닌 곳 — 기존 파일 1개.
	dstDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dstDir, "preexisting.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed pre-existing file: %v", err)
	}

	rRes := runRestoreCapture(t, []string{"--input", outPath, "--data-dir", dstDir})
	if rRes.exit == 0 {
		t.Fatalf("restore should fail on non-empty dir; got exit=0; stdout=%s", rRes.stdout)
	}
}

func TestRestoreForceAcceptsNonEmptyDir(t *testing.T) {
	srcDir := t.TempDir()
	_, _ = seedTenantForBackup(t, srcDir, "force@example.com")

	outPath := filepath.Join(t.TempDir(), "backup.tar.gz")
	bRes := runBackupCapture(t, []string{"--output", outPath, "--data-dir", srcDir})
	if bRes.exit != 0 {
		t.Fatalf("backup exit=%d, want 0; stderr=%s", bRes.exit, bRes.stderr)
	}

	dstDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dstDir, "stale.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed pre-existing: %v", err)
	}

	rRes := runRestoreCapture(t, []string{"--input", outPath, "--data-dir", dstDir, "--force"})
	if rRes.exit != 0 {
		t.Fatalf("restore --force exit=%d, want 0; stderr=%s", rRes.exit, rRes.stderr)
	}
	// data.db 존재 확인.
	if _, err := os.Stat(filepath.Join(dstDir, "data.db")); err != nil {
		t.Errorf("restored data.db missing: %v", err)
	}
}

func TestRestoreRejectsCorruptedTar(t *testing.T) {
	dstDir := t.TempDir()
	bogus := filepath.Join(t.TempDir(), "bogus.tar.gz")
	if err := os.WriteFile(bogus, []byte("not a real gzip stream"), 0o644); err != nil {
		t.Fatalf("write bogus: %v", err)
	}
	rRes := runRestoreCapture(t, []string{"--input", bogus, "--data-dir", dstDir})
	if rRes.exit == 0 {
		t.Fatalf("restore should fail on corrupted tar; got exit=0; stdout=%s", rRes.stdout)
	}
}

func TestBackupHelpExitsZero(t *testing.T) {
	res := runBackupCapture(t, []string{"-h"})
	if res.exit != 0 {
		t.Fatalf("backup -h exit=%d, want 0", res.exit)
	}
}

func TestRestoreHelpExitsZero(t *testing.T) {
	res := runRestoreCapture(t, []string{"-h"})
	if res.exit != 0 {
		t.Fatalf("restore -h exit=%d, want 0", res.exit)
	}
}

// === 헬퍼 ===

// domainCounts는 fresh Bootstrap으로 tenants·users count를 반환합니다.
// tenantID는 시드된 admin tenant ID(추가 검증 — admin user 존재 확인용).
func domainCounts(t *testing.T, dataDir, tenantID string) (int, int) {
	t.Helper()
	p, err := Bootstrap(context.Background(), Config{
		DataDir: dataDir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("domainCounts Bootstrap (%s): %v", dataDir, err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	var tn, us int
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM tenants`).Scan(&tn); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM users WHERE tenant_id = ?`, tenantID).Scan(&us); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("domainCounts query: %v", err)
	}
	return tn, us
}
