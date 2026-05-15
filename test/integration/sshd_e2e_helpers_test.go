//go:build integration

// sshd_e2e_helpers_test.go — sshd_e2e_test.go가 사용하는 harness/어댑터 헬퍼.
//
// 본 파일은 build tag `integration` 빌드 시점에만 컴파일됩니다 — sshd_e2e_test.go와
// 같은 시점에 컴파일되도록 같은 build tag 부착. 파일 분리는 800-line cap (CLAUDE.md)
// 회피 + 테스트 vs harness 가독성 분리 목적.
package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
	"github.com/ssabro/rosshield/internal/domain/scan"
	scanrepo "github.com/ssabro/rosshield/internal/domain/scan/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	pkglogger "github.com/ssabro/rosshield/internal/platform/logger"
	"github.com/ssabro/rosshield/internal/platform/sshpool"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === 컨테이너 fixture 상수 ===
//
// docker-compose.ssh.yml과 정확 일치 — 변경 시 양쪽 동기화.
const (
	fixtureUsername = "rosshield"
	fixturePassword = "e2e-test-password"
	fixtureHost     = "127.0.0.1"

	robot1Port = 12222
	robot2Port = 12223
	robot3Port = 12224

	// 컨테이너 기동·재기동 대기 — healthcheck interval 5s × retries 5 = 25s.
	// 본 테스트는 기동 직후 dial 가능까지 polling.
	containerReadyTimeout = 30 * time.Second
)

// === 컨테이너 lifecycle 헬퍼 ===

// dockerComposeFile은 본 패키지 디렉토리의 yaml 절대 경로를 반환합니다.
func dockerComposeFile(t *testing.T) string {
	t.Helper()
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	return filepath.Join(cwd, "docker-compose.ssh.yml")
}

// composeCmd는 `docker compose -f <file> <args...>` 명령을 실행합니다.
func composeCmd(t *testing.T, args ...string) error {
	t.Helper()
	all := append([]string{"compose", "-f", dockerComposeFile(t)}, args...)
	cmd := exec.Command("docker", all...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %v\n%s", strings.Join(all, " "), err, string(out))
	}
	return nil
}

// waitForPort는 host:port에 TCP dial 가능할 때까지 polling합니다.
func waitForPort(t *testing.T, host string, port int, timeout time.Duration) error {
	t.Helper()
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("waitForPort: %s not reachable in %s", addr, timeout)
}

// requireDocker는 docker CLI가 PATH에 있고 daemon이 응답하는지 확인합니다.
func requireDocker(t *testing.T) {
	t.Helper()
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").CombinedOutput()
	if err != nil {
		t.Skipf("docker not available — skipping e2e: %v\n%s", err, string(out))
	}
}

// === scan 도메인 결선 헬퍼 ===
//
// scanrun_test.go의 harness 패턴과 같지만 본 패키지에서 import할 수 없으므로
// 같은 결선 코드를 작성 — 통합 테스트 한정.

// auditEmitter는 scan.AuditEmitter를 audit.Service에 어댑팅합니다 (test 한정).
type auditEmitter struct {
	svc audit.Service
}

func (a *auditEmitter) EmitScanStarted(ctx context.Context, tx storage.Tx, s scan.ScanSession) error {
	return a.append(ctx, tx, s, "scan.started")
}

func (a *auditEmitter) EmitScanCompleted(ctx context.Context, tx storage.Tx, s scan.ScanSession) error {
	return a.append(ctx, tx, s, "scan.completed")
}

func (a *auditEmitter) EmitScanFailed(ctx context.Context, tx storage.Tx, s scan.ScanSession, _ string) error {
	return a.append(ctx, tx, s, "scan.failed")
}

func (a *auditEmitter) EmitScanCancelled(ctx context.Context, tx storage.Tx, s scan.ScanSession, _ string) error {
	return a.append(ctx, tx, s, "scan.cancelled")
}

func (a *auditEmitter) append(ctx context.Context, tx storage.Tx, s scan.ScanSession, action string) error {
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: s.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   action,
		Target:   audit.Target{Type: "scan_session", ID: s.ID},
		Payload:  []byte(`{}`),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// e2eHarness는 본 e2e 통합 테스트용 결선 묶음입니다.
type e2eHarness struct {
	t        *testing.T
	store    storage.Storage
	scanSvc  scan.Service
	bus      eventbus.Bus
	tenantID storage.TenantID
	fleetID  string
	packID   string
}

func newE2EHarness(t *testing.T) *e2eHarness {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "scanrun-e2e.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clock.System()})
	scanSvc := scanrepo.New(scanrepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
		Audit: &auditEmitter{svc: auditSvc},
	})

	bus := inproc.New(inproc.Deps{
		Logger: pkglogger.New(io.Discard, nil),
		Clock:  clock.System(),
		IDGen:  idgen.NewULID(),
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = bus.Close(ctx)
	})

	return &e2eHarness{t: t, store: store, scanSvc: scanSvc, bus: bus}
}

func (h *e2eHarness) seedFleetAndPack(tenantID, fleetID, packID string) {
	h.t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := h.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'e2e', 'desktop_free', ?)`, tenantID, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at) VALUES (?, ?, 'fleet', '', '{}', ?, ?)`,
			fleetID, tenantID, now, now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO packs (id, tenant_id, name, version, vendor, pack_key, manifest_hash, signer_key_id, installed_at)
VALUES (?, ?, 'pk', 'v1', 'CIS', 'key', x'00', 'key_test', ?)`, packID, tenantID, now)
		return err
	}); err != nil {
		h.t.Fatalf("seedFleetAndPack: %v", err)
	}
	h.tenantID = storage.TenantID(tenantID)
	h.fleetID = fleetID
	h.packID = packID
}

// seedRobots는 N개의 robot을 INSERT합니다 (host·port 명시).
func (h *e2eHarness) seedRobots(targets []scan.RobotTarget) {
	h.t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := h.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO credentials (id, tenant_id, type, encrypted_payload, encryption_meta, rotation_due_at, created_at, updated_at, revoked_at)
VALUES ('cr_x', ?, 'password', x'00', '{}', NULL, ?, ?, NULL)`, string(h.tenantID), now, now); err != nil {
			return err
		}
		for _, t := range targets {
			if _, err := tx.Exec(ctx, `INSERT INTO robots (id, tenant_id, fleet_id, credential_id, name, host, port, auth_type, os_distro, ros_distro, tags, role, criticality, created_at, updated_at, last_scan_at, deleted_at)
VALUES (?, ?, ?, 'cr_x', ?, ?, ?, 'password', '', '', '[]', '', 'medium', ?, ?, NULL, NULL)`,
				t.RobotID, string(h.tenantID), h.fleetID, t.RobotID, t.Host, t.Port, now, now); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		h.t.Fatalf("seedRobots: %v", err)
	}
}

func (h *e2eHarness) seedChecks(checks []scan.CheckDef) {
	h.t.Helper()
	if err := h.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		for _, c := range checks {
			if _, err := tx.Exec(ctx, `INSERT INTO pack_checks (id, pack_id, check_id, title, severity, evaluation_rule)
VALUES (?, ?, ?, 't', 'medium', ?)`,
				c.PackCheckID, h.packID, c.Code, string(c.EvalRuleJSON)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		h.t.Fatalf("seedChecks: %v", err)
	}
}

func (h *e2eHarness) startSession(total int) string {
	h.t.Helper()
	var sessionID string
	if err := h.store.Tx(storage.WithTenantID(context.Background(), h.tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			s, err := h.scanSvc.StartScan(ctx, tx, scan.StartScanRequest{
				FleetID: h.fleetID, PackID: h.packID, Trigger: scan.TriggerManual, Total: total,
			})
			sessionID = s.ID
			return err
		}); err != nil {
		h.t.Fatalf("StartScan: %v", err)
	}
	return sessionID
}

func (h *e2eHarness) reload(sessionID string) scan.ScanSession {
	h.t.Helper()
	var s scan.ScanSession
	if err := h.store.Tx(storage.WithTenantID(context.Background(), h.tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			r, err := h.scanSvc.GetSession(ctx, tx, sessionID)
			s = r
			return err
		}); err != nil {
		h.t.Fatalf("GetSession: %v", err)
	}
	return s
}

func (h *e2eHarness) listResults(sessionID string) []scan.ScanResult {
	h.t.Helper()
	var rs []scan.ScanResult
	if err := h.store.Tx(storage.WithTenantID(context.Background(), h.tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			r, err := h.scanSvc.ListResults(ctx, tx, sessionID)
			rs = r
			return err
		}); err != nil {
		h.t.Fatalf("ListResults: %v", err)
	}
	return rs
}

// listAuditActions는 sessionID의 audit actions를 raw SELECT으로 조회합니다.
func (h *e2eHarness) listAuditActions(sessionID string) []string {
	h.t.Helper()
	var actions []string
	if err := h.store.Tx(storage.WithTenantID(context.Background(), h.tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			rows, err := tx.Query(ctx, `SELECT action FROM audit_entries WHERE target_type=? AND target_id=? ORDER BY seq ASC`,
				"scan_session", sessionID)
			if err != nil {
				return err
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var a string
				if err := rows.Scan(&a); err != nil {
					return err
				}
				actions = append(actions, a)
			}
			return rows.Err()
		}); err != nil {
		h.t.Fatalf("listAuditActions: %v", err)
	}
	return actions
}

// === e2e용 SSHExecutor 어댑터 ===
//
// scanexec.go의 sshExecutorAdapter와 동일 책임이지만 cmd/* main 패키지를 import
// 불가하므로 본 파일에 동일 결선 — password 인증 직접 구성, host key callback은
// 가변(InsecureIgnoreHostKey 또는 외부 callback 주입 가능 — Phase 4용).
type e2eSSHAdapter struct {
	pool sshpool.Executor

	// hostKeyCB가 nil이면 InsecureIgnoreHostKey() — Phase 1·2·3 기본.
	// Phase 4(host key change)는 fingerprint pin 기반 callback을 주입.
	hostKeyCB ssh.HostKeyCallback
}

func (a *e2eSSHAdapter) Exec(ctx context.Context, target scan.RobotTarget, argv []string, timeout time.Duration, opts scan.ExecOpts) (scan.ExecResult, error) {
	cb := a.hostKeyCB
	if cb == nil {
		cb = ssh.InsecureIgnoreHostKey()
	}
	sudoMode := sshpool.SudoNone
	if opts.RequiresSudo {
		sudoMode = sshpool.SudoNonInteractive
	}
	res, err := a.pool.Exec(ctx, sshpool.Target{
		Host:            target.Host,
		Port:            target.Port,
		Username:        fixtureUsername,
		Auth:            ssh.Password(fixturePassword),
		HostKeyCallback: cb,
		SudoMode:        sudoMode,
	}, argv, timeout)
	return scan.ExecResult{
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
		ExitCode: res.ExitCode,
		Duration: res.Duration,
	}, err
}

// === e2e용 sealed AST evaluator 어댑터 ===
type e2eEvaluator struct{}

func (e2eEvaluator) Evaluate(ruleJSON []byte, exec scan.ExecResult) (scan.EvalResult, error) {
	node, err := benchmark.ParseEvalRule(json.RawMessage(ruleJSON))
	if err != nil {
		return scan.EvalResult{}, fmt.Errorf("e2e eval parse: %w", err)
	}
	res, err := node.Eval(benchmark.EvalInput{
		Stdout:   string(exec.Stdout),
		Stderr:   string(exec.Stderr),
		ExitCode: exec.ExitCode,
	})
	if err != nil {
		return scan.EvalResult{}, fmt.Errorf("e2e eval: %w", err)
	}
	switch res.Status {
	case benchmark.StatusPass:
		return scan.EvalResult{Outcome: scan.OutcomePass, Reason: res.Reason}, nil
	case benchmark.StatusFail:
		return scan.EvalResult{Outcome: scan.OutcomeFail, Reason: res.Reason}, nil
	case benchmark.StatusIndeterminate:
		return scan.EvalResult{Outcome: scan.OutcomeIndeterminate, Reason: res.Reason}, nil
	default:
		return scan.EvalResult{Outcome: scan.OutcomeError, Reason: res.Reason}, nil
	}
}

// === check 정의 헬퍼 ===
//
// linuxserver/openssh-server 컨테이너에서 stdout이 알 수 있는 cmd만 사용 — `echo`,
// `uname`, `id`, `hostname` 등. CIS-style check ID를 부여하지만 실 CIS pack과
// 결합하지 않음 — sealed AST evaluator 결선 검증이 목적.

func cisCheck3() []scan.CheckDef {
	return []scan.CheckDef{
		{
			PackCheckID:  "ck_e2e_001",
			Code:         "E2E-CIS-1.1",
			AuditCommand: []string{"echo", "PermitRootLogin no"},
			TimeoutSec:   5,
			EvalRuleJSON: []byte(`{"op":"contains","value":"PermitRootLogin no"}`),
		},
		{
			PackCheckID:  "ck_e2e_002",
			Code:         "E2E-CIS-1.2",
			AuditCommand: []string{"echo", "Port 22"},
			TimeoutSec:   5,
			EvalRuleJSON: []byte(`{"op":"regex","pattern":"(?m)^Port\\s+22$"}`),
		},
		{
			PackCheckID:  "ck_e2e_003",
			Code:         "E2E-CIS-1.3",
			AuditCommand: []string{"id", "-un"},
			TimeoutSec:   5,
			// rosshield user expected.
			EvalRuleJSON: []byte(`{"op":"contains","value":"rosshield"}`),
		},
	}
}

func cisCheck5() []scan.CheckDef {
	out := cisCheck3()
	out = append(out,
		scan.CheckDef{
			PackCheckID:  "ck_e2e_004",
			Code:         "E2E-CIS-1.4",
			AuditCommand: []string{"hostname"},
			TimeoutSec:   5,
			// hostname starts with "rosshield-robot-".
			EvalRuleJSON: []byte(`{"op":"contains","value":"rosshield-robot"}`),
		},
		scan.CheckDef{
			PackCheckID:  "ck_e2e_005",
			Code:         "E2E-CIS-1.5",
			AuditCommand: []string{"uname", "-s"},
			TimeoutSec:   5,
			EvalRuleJSON: []byte(`{"op":"contains","value":"Linux"}`),
		},
	)
	return out
}

func robotTarget(id string, port int) scan.RobotTarget {
	return scan.RobotTarget{
		RobotID:      id,
		Host:         fixtureHost,
		Port:         port,
		AuthType:     "password",
		CredentialID: "cr_x",
	}
}
