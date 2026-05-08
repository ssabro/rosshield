package main

// backup_test.go — `rosshield-server backup` 서브커맨드 단위 테스트 (E28 T2/T4).
//
// 시나리오:
//
//	T1·T2 round-trip: backup → restore → 도메인 read 동등성은 restore_test.go에서 검증.
//	T2 consistent snapshot: SQLite VACUUM INTO 사용 — backup이 서버 라이프사이클과
//	  무관하게 최신 commit의 일관 스냅샷을 떠내는지(트랜잭션 중간 상태 노출 X) 확인.
//	T4 --skip-evidence: tar 본문에 evidence/ 미포함.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/evidence"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// backupRun은 stdout/stderr·exit code 캡처 결과입니다.
type backupRun struct {
	exit   int
	stdout string
	stderr string
}

// runBackupCapture는 stdout/stderr·exit code를 캡처합니다.
func runBackupCapture(t *testing.T, args []string) backupRun {
	t.Helper()
	oldStdout, oldStderr := os.Stdout, os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	exit := backupSubcommand(args)

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var bufOut, bufErr bytes.Buffer
	_, _ = io.Copy(&bufOut, rOut)
	_, _ = io.Copy(&bufErr, rErr)

	return backupRun{exit: exit, stdout: bufOut.String(), stderr: bufErr.String()}
}

// seedTenantForBackup은 backup 테스트용 admin tenant + evidence를 시드합니다.
// evidence는 Phase 2 demo seed처럼 본격 결선이 아니라, blobstore 디렉터리에 1개 blob을
// 남기기 위해 evidence.Service.Store를 직접 호출합니다 (tar에 evidence/ 포함 검증용).
func seedTenantForBackup(t *testing.T, dataDir, email string) (tenantID string, evidenceSHA string) {
	t.Helper()
	res := runSeedCapture(t, []string{
		"admin",
		"--email", email,
		"--password", "verylongpassword1",
		"--data-dir", dataDir,
		"--name", "Backup Test Tenant",
	}, "")
	if res.exit != 0 {
		t.Fatalf("seed admin exit=%d, want 0; stderr=%s", res.exit, res.stderr)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("unmarshal seed stdout: %v\nraw: %s", err, res.stdout)
	}
	tenantID, _ = out["tenantId"].(string)

	// Evidence 1개 추가 — backup tar에 evidence/ 디렉터리가 포함되는지 검증.
	p, err := Bootstrap(context.Background(), Config{
		DataDir: dataDir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("evidence seed bootstrap: %v", err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	tid := storage.TenantID(tenantID)
	ctx := storage.WithTenantID(context.Background(), tid)
	body := []byte("hello-backup-evidence-blob")
	sum := sha256.Sum256(body)
	evidenceSHA = hex.EncodeToString(sum[:])

	if err := p.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := p.Evidence.Store(ctx, tx, evidence.StoreInput{
			TenantID:    tid,
			ContentType: evidence.ContentStdout,
			Raw:         body,
		})
		return err
	}); err != nil {
		t.Fatalf("evidence Store: %v", err)
	}

	return tenantID, evidenceSHA
}

func TestBackupCreatesGzipTarball(t *testing.T) {
	dataDir := t.TempDir()
	_, _ = seedTenantForBackup(t, dataDir, "admin@example.com")

	outPath := filepath.Join(t.TempDir(), "backup.tar.gz")
	res := runBackupCapture(t, []string{"--output", outPath, "--data-dir", dataDir})
	if res.exit != 0 {
		t.Fatalf("exit=%d, want 0; stderr=%s", res.exit, res.stderr)
	}

	st, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("backup file not created: %v", err)
	}
	if st.Size() == 0 {
		t.Fatal("backup file is empty")
	}

	// stdout JSON 검증.
	var out map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("unmarshal stdout: %v\nraw: %s", err, res.stdout)
	}
	for _, k := range []string{"path", "size", "sha256", "includesEvidence"} {
		if _, ok := out[k]; !ok {
			t.Errorf("missing JSON key %q in %s", k, res.stdout)
		}
	}
	if got, _ := out["includesEvidence"].(bool); !got {
		t.Errorf("includesEvidence=false, want true")
	}
}

// TestBackupContainsExpectedEntries는 tar 안에 data.db + keys/* + evidence/* 가
// 들어있는지 한 번에 검증합니다 (T1 round-trip의 사전 단계 — 파일 누락 빨리 발견).
func TestBackupContainsExpectedEntries(t *testing.T) {
	dataDir := t.TempDir()
	_, evidenceSHA := seedTenantForBackup(t, dataDir, "admin@example.com")

	outPath := filepath.Join(t.TempDir(), "backup.tar.gz")
	res := runBackupCapture(t, []string{"--output", outPath, "--data-dir", dataDir})
	if res.exit != 0 {
		t.Fatalf("exit=%d, want 0; stderr=%s", res.exit, res.stderr)
	}

	entries := readTarEntries(t, outPath)

	// data.db 존재 확인.
	if _, ok := entries["data.db"]; !ok {
		t.Errorf("backup tar missing data.db; entries=%v", keysOf(entries))
	}
	// keys/platform.ed25519 존재 확인 (signer key — bootstrap에서 자동 생성).
	if _, ok := entries["keys/platform.ed25519"]; !ok {
		t.Errorf("backup tar missing keys/platform.ed25519; entries=%v", keysOf(entries))
	}
	// evidence blob 존재 확인 — sha256[0:2]/[2:4] shard 구조.
	wantBlob := "evidence/" + evidenceSHA[0:2] + "/" + evidenceSHA[2:4] + "/" + evidenceSHA + ".blob"
	if _, ok := entries[wantBlob]; !ok {
		t.Errorf("backup tar missing %s; entries=%v", wantBlob, keysOf(entries))
	}
}

// TestBackupSkipEvidence는 --skip-evidence 옵션이 evidence/ 디렉터리 전체를
// tar에서 제거하는지 검증합니다 (T4).
func TestBackupSkipEvidence(t *testing.T) {
	dataDir := t.TempDir()
	_, _ = seedTenantForBackup(t, dataDir, "admin@example.com")

	outPath := filepath.Join(t.TempDir(), "backup-no-evidence.tar.gz")
	res := runBackupCapture(t, []string{
		"--output", outPath,
		"--data-dir", dataDir,
		"--skip-evidence",
	})
	if res.exit != 0 {
		t.Fatalf("exit=%d, want 0; stderr=%s", res.exit, res.stderr)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("unmarshal stdout: %v", err)
	}
	if got, _ := out["includesEvidence"].(bool); got {
		t.Errorf("includesEvidence=true with --skip-evidence")
	}

	entries := readTarEntries(t, outPath)
	for name := range entries {
		if strings.HasPrefix(name, "evidence/") {
			t.Errorf("evidence entry leaked despite --skip-evidence: %s", name)
		}
	}
}

// TestBackupSnapshotConsistentDuringWrites는 backup이 SQLite VACUUM INTO 기반으로
// 일관 스냅샷을 떠내는지 검증합니다 (T2). backup 호출 후 원본 DB에 추가 변경을 가해도
// 스냅샷의 row count는 backup 시점의 값과 일치해야 합니다.
func TestBackupSnapshotConsistentDuringWrites(t *testing.T) {
	dataDir := t.TempDir()
	_, _ = seedTenantForBackup(t, dataDir, "admin@example.com")

	// backup 직전 tenant count 캡처.
	before := countTenants(t, dataDir)

	outPath := filepath.Join(t.TempDir(), "backup-consistent.tar.gz")
	res := runBackupCapture(t, []string{"--output", outPath, "--data-dir", dataDir})
	if res.exit != 0 {
		t.Fatalf("exit=%d, want 0; stderr=%s", res.exit, res.stderr)
	}

	// backup 후 원본 DB에 mutation — Phase 2 demo seed로 인서트 추가.
	// (admin 시드가 끝난 상태에서 재현 가능한 변경: invitation 토큰 row 추가는 Bootstrap+Tx 필요)
	// 가장 간단한 변경: 추가 admin tenant 시드 시도는 거부됨 — 대신 임의 row 직접 INSERT로
	// "이후 변경"을 시뮬레이션. 두 번째 tenant를 직접 SQL로 인서트 (테스트 hack).
	insertExtraTenant(t, dataDir, "extra-tenant-after-backup")

	afterOriginal := countTenants(t, dataDir)
	if afterOriginal != before+1 {
		t.Fatalf("original DB tenant count after extra insert=%d, want %d", afterOriginal, before+1)
	}

	// 백업을 새 디렉터리에 복원해 snapshot의 tenant count 확인.
	restoreDir := t.TempDir()
	rRes := runRestoreCapture(t, []string{"--input", outPath, "--data-dir", restoreDir})
	if rRes.exit != 0 {
		t.Fatalf("restore exit=%d, want 0; stderr=%s", rRes.exit, rRes.stderr)
	}
	snapshotCount := countTenants(t, restoreDir)
	if snapshotCount != before {
		t.Fatalf("snapshot tenant count=%d, want %d (consistent with backup time)", snapshotCount, before)
	}
}

// === 헬퍼 ===

func readTarEntries(t *testing.T, path string) map[string]int64 {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	out := make(map[string]int64)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar Next: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			out[hdr.Name] = hdr.Size
		}
	}
	return out
}

func keysOf(m map[string]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func countTenants(t *testing.T, dataDir string) int {
	t.Helper()
	p, err := Bootstrap(context.Background(), Config{
		DataDir: dataDir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("countTenants Bootstrap: %v", err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	var n int
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		return tx.QueryRow(ctx, `SELECT COUNT(*) FROM tenants`).Scan(&n)
	}); err != nil {
		t.Fatalf("countTenants query: %v", err)
	}
	return n
}

// insertExtraTenant는 SQL 직접 INSERT로 임의 tenant row를 추가합니다 (T2 mutation 시뮬레이션).
// 도메인 검증을 우회하므로 테스트 전용 — VACUUM INTO snapshot 일관성 검증이 목적.
func insertExtraTenant(t *testing.T, dataDir, name string) {
	t.Helper()
	p, err := Bootstrap(context.Background(), Config{
		DataDir: dataDir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("insertExtraTenant Bootstrap: %v", err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		// tenants 스키마(0003 마이그레이션): id PK, name, plan default, created_at,
		// settings/features/retention default — 최소 컬럼 3개만 지정.
		_, err := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?)`,
			"tn_TEST_EXTRA", name, "2026-01-01T00:00:00Z")
		return err
	}); err != nil {
		t.Fatalf("insert extra tenant: %v", err)
	}
}
