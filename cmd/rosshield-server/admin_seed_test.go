package main

// admin_seed_test.go — `seed admin` CLI 서브커맨드 단위 테스트 (Phase 1 Exit 데모).
//
// 시나리오 매트릭스: 정상 흐름·필수 옵션 누락·짧은 패스워드·중복 시드·커스텀 ID/이름·
// audit chain 증가·stdin 패스워드. exit code + JSON stdout + DB row 검증을 한 번에.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// seedRun은 stdout/stderr·exit code 캡쳐 결과입니다.
type seedRun struct {
	exit   int
	stdout string
	stderr string
}

// runSeedCapture는 stdout/stderr·exit code·stdin 주입을 한 번에 캡쳐합니다.
//
// stdin이 비어있지 않으면 별도 pipe로 주입(--password-stdin 테스트용).
func runSeedCapture(t *testing.T, args []string, stdin string) seedRun {
	t.Helper()
	oldStdout, oldStderr, oldStdin := os.Stdout, os.Stderr, os.Stdin

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	if stdin != "" {
		rIn, wIn, _ := os.Pipe()
		os.Stdin = rIn
		go func() {
			_, _ = io.WriteString(wIn, stdin)
			_ = wIn.Close()
		}()
	}

	exit := seedSubcommand(args)

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	os.Stdin = oldStdin

	var bufOut, bufErr bytes.Buffer
	_, _ = io.Copy(&bufOut, rOut)
	_, _ = io.Copy(&bufErr, rErr)

	return seedRun{exit: exit, stdout: bufOut.String(), stderr: bufErr.String()}
}

func TestSeedAdminCreatesTenantAndAdminUser(t *testing.T) {
	dir := t.TempDir()
	res := runSeedCapture(t, []string{
		"admin",
		"--email", "admin@example.com",
		"--password", "verylongpassword1",
		"--data-dir", dir,
		"--name", "Default Tenant",
	}, "")
	if res.exit != 0 {
		t.Fatalf("exit=%d, want 0; stdout=%s; stderr=%s", res.exit, res.stdout, res.stderr)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("unmarshal stdout: %v\nraw: %s", err, res.stdout)
	}
	for _, k := range []string{"tenantId", "tenantName", "userId", "email", "seededAt"} {
		if _, ok := out[k]; !ok {
			t.Errorf("missing JSON key %q in %s", k, res.stdout)
		}
	}
	if email, _ := out["email"].(string); email != "admin@example.com" {
		t.Errorf("email=%q, want admin@example.com", email)
	}
	if name, _ := out["tenantName"].(string); name != "Default Tenant" {
		t.Errorf("tenantName=%q, want 'Default Tenant'", name)
	}
	uid, _ := out["userId"].(string)
	if !strings.HasPrefix(uid, "us_") {
		t.Errorf("userId=%q, want 'us_' prefix", uid)
	}
	tid, _ := out["tenantId"].(string)
	if !strings.HasPrefix(tid, "tn_") {
		t.Errorf("tenantId=%q, want 'tn_' prefix", tid)
	}

	verifySeededRows(t, dir, tid, "admin@example.com")
}

func TestSeedAdminRejectsMissingEmail(t *testing.T) {
	dir := t.TempDir()
	res := runSeedCapture(t, []string{
		"admin", "--password", "verylongpassword1", "--data-dir", dir,
	}, "")
	if res.exit != 2 {
		t.Fatalf("exit=%d, want 2; stderr=%s", res.exit, res.stderr)
	}
}

func TestSeedAdminRejectsMissingPassword(t *testing.T) {
	dir := t.TempDir()
	res := runSeedCapture(t, []string{
		"admin", "--email", "admin@example.com", "--data-dir", dir,
	}, "")
	if res.exit != 2 {
		t.Fatalf("exit=%d, want 2; stderr=%s", res.exit, res.stderr)
	}
}

func TestSeedAdminRejectsShortPassword(t *testing.T) {
	dir := t.TempDir()
	res := runSeedCapture(t, []string{
		"admin", "--email", "admin@example.com", "--password", "short", "--data-dir", dir,
	}, "")
	if res.exit != 2 {
		t.Fatalf("exit=%d, want 2; stderr=%s", res.exit, res.stderr)
	}
}

func TestSeedAdminRejectsInvalidEmail(t *testing.T) {
	dir := t.TempDir()
	res := runSeedCapture(t, []string{
		"admin", "--email", "not-an-email", "--password", "verylongpassword1", "--data-dir", dir,
	}, "")
	if res.exit != 2 {
		t.Fatalf("exit=%d, want 2; stderr=%s", res.exit, res.stderr)
	}
}

func TestSeedAdminRejectsDuplicateSeed(t *testing.T) {
	dir := t.TempDir()
	first := runSeedCapture(t, []string{
		"admin", "--email", "admin@example.com", "--password", "verylongpassword1", "--data-dir", dir,
	}, "")
	if first.exit != 0 {
		t.Fatalf("first seed exit=%d, want 0; stderr=%s", first.exit, first.stderr)
	}
	second := runSeedCapture(t, []string{
		"admin", "--email", "admin@example.com", "--password", "verylongpassword1", "--data-dir", dir,
	}, "")
	if second.exit != 3 {
		t.Fatalf("second seed exit=%d, want 3 (duplicate); stderr=%s", second.exit, second.stderr)
	}
}

func TestSeedAdminAcceptsCustomTenantNameAndDisplayName(t *testing.T) {
	dir := t.TempDir()
	res := runSeedCapture(t, []string{
		"admin",
		"--email", "ops@example.com",
		"--password", "verylongpassword1",
		"--data-dir", dir,
		"--name", "My Org",
		"--display-name", "Ops Lead",
	}, "")
	if res.exit != 0 {
		t.Fatalf("exit=%d, want 0; stderr=%s", res.exit, res.stderr)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("unmarshal stdout: %v\nraw: %s", err, res.stdout)
	}
	if name, _ := out["tenantName"].(string); name != "My Org" {
		t.Errorf("tenantName=%q, want 'My Org'", name)
	}
}

func TestSeedAdminEmitsAuditEntry(t *testing.T) {
	dir := t.TempDir()
	res := runSeedCapture(t, []string{
		"admin", "--email", "admin@example.com", "--password", "verylongpassword1", "--data-dir", dir,
	}, "")
	if res.exit != 0 {
		t.Fatalf("exit=%d, want 0; stderr=%s", res.exit, res.stderr)
	}
	count, action := countAuditEntriesForLatestTenant(t, dir)
	if count != 1 {
		t.Fatalf("audit_entries count=%d, want 1", count)
	}
	if action != "tenant.created" {
		t.Errorf("action=%q, want tenant.created", action)
	}
}

func TestSeedAdminPasswordStdin(t *testing.T) {
	dir := t.TempDir()
	res := runSeedCapture(t, []string{
		"admin", "--email", "stdin@example.com", "--password-stdin", "--data-dir", dir,
	}, "verylongpassword1\n")
	if res.exit != 0 {
		t.Fatalf("exit=%d, want 0; stdout=%s; stderr=%s", res.exit, res.stdout, res.stderr)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("unmarshal stdout: %v\nraw: %s", err, res.stdout)
	}
	if email, _ := out["email"].(string); email != "stdin@example.com" {
		t.Errorf("email=%q, want stdin@example.com", email)
	}
}

func TestSeedAdminHelpExitsZero(t *testing.T) {
	res := runSeedCapture(t, []string{"help"}, "")
	if res.exit != 0 {
		t.Fatalf("help exit=%d, want 0", res.exit)
	}
	res2 := runSeedCapture(t, []string{"-h"}, "")
	if res2.exit != 0 {
		t.Fatalf("-h exit=%d, want 0", res2.exit)
	}
}

func TestSeedAdminUnknownSubcommand(t *testing.T) {
	res := runSeedCapture(t, []string{"unknown"}, "")
	if res.exit != 2 {
		t.Fatalf("unknown exit=%d, want 2", res.exit)
	}
}

// === DB 검증 헬퍼 ===

// verifySeededRows는 fresh Bootstrap으로 DB를 재오픈해 시드된 row를 직접 SELECT 검증합니다.
func verifySeededRows(t *testing.T, dataDir, tenantID, email string) {
	t.Helper()
	p, err := Bootstrap(context.Background(), Config{
		DataDir: dataDir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("verify Bootstrap: %v", err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		var name string
		if err := tx.QueryRow(ctx, `SELECT name FROM tenants WHERE id = ?`, tenantID).Scan(&name); err != nil {
			t.Errorf("tenant row not found: %v", err)
		}
		var userID, authProvider, status string
		if err := tx.QueryRow(ctx,
			`SELECT id, auth_provider, status FROM users WHERE tenant_id = ? AND email = ?`,
			tenantID, email).Scan(&userID, &authProvider, &status); err != nil {
			t.Errorf("user row not found: %v", err)
		}
		if authProvider != "local" {
			t.Errorf("user.auth_provider=%q, want local", authProvider)
		}
		if status != "active" {
			t.Errorf("user.status=%q, want active", status)
		}
		var roleCount int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM roles WHERE tenant_id = ? AND is_system = 1`,
			tenantID).Scan(&roleCount); err != nil {
			t.Errorf("roles count: %v", err)
		}
		if roleCount != 3 {
			t.Errorf("system role count=%d, want 3 (admin/auditor/operator)", roleCount)
		}
		var assigned int
		if err := tx.QueryRow(ctx, `
SELECT COUNT(*) FROM user_roles ur
  JOIN roles r ON r.id = ur.role_id
 WHERE ur.user_id = ? AND r.name = 'admin'`, userID).Scan(&assigned); err != nil {
			t.Errorf("user_role assigned: %v", err)
		}
		if assigned != 1 {
			t.Errorf("admin role assignment count=%d, want 1", assigned)
		}
		return nil
	}); err != nil {
		t.Fatalf("verify Bootstrap Tx: %v", err)
	}
}

// countAuditEntriesForLatestTenant는 가장 최근 시드된 tenant의 audit_entries를 카운트하고
// 그 안에 어떤 action이 들어있는지 반환합니다 (단일 row 기대 — tenant.created 1건).
func countAuditEntriesForLatestTenant(t *testing.T, dataDir string) (int, string) {
	t.Helper()
	p, err := Bootstrap(context.Background(), Config{
		DataDir: dataDir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("audit-check Bootstrap: %v", err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	var count int
	var action string
	if err := p.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		var tid string
		if err := tx.QueryRow(ctx,
			`SELECT id FROM tenants ORDER BY created_at DESC LIMIT 1`).Scan(&tid); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*), COALESCE(MIN(action),'') FROM audit_entries WHERE tenant_id = ?`,
			tid).Scan(&count, &action); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	return count, action
}
