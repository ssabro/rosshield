package scanrun_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/app/scanrun"
	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/scan"
	scanrepo "github.com/ssabro/rosshield/internal/domain/scan/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	pkglogger "github.com/ssabro/rosshield/internal/platform/logger"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === scan.AuditEmitter 테스트 어댑터 ===

type auditAdapter struct {
	svc audit.Service
}

func (a *auditAdapter) EmitScanStarted(ctx context.Context, tx storage.Tx, s scan.ScanSession) error {
	return a.append(ctx, tx, s, "scan.started")
}
func (a *auditAdapter) EmitScanCompleted(ctx context.Context, tx storage.Tx, s scan.ScanSession) error {
	return a.append(ctx, tx, s, "scan.completed")
}
func (a *auditAdapter) EmitScanFailed(ctx context.Context, tx storage.Tx, s scan.ScanSession, _ string) error {
	return a.append(ctx, tx, s, "scan.failed")
}
func (a *auditAdapter) EmitScanCancelled(ctx context.Context, tx storage.Tx, s scan.ScanSession, _ string) error {
	return a.append(ctx, tx, s, "scan.cancelled")
}
func (a *auditAdapter) append(ctx context.Context, tx storage.Tx, s scan.ScanSession, action string) error {
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

// === Mock SSHExecutor ===

type mockExecutor struct {
	exec func(ctx context.Context, target scan.RobotTarget, argv []string, timeout time.Duration) (scan.ExecResult, error)

	// 동시성 추적 (T6)
	active     int64
	peakActive int64
	totalCalls int64
}

func (m *mockExecutor) Exec(ctx context.Context, target scan.RobotTarget, argv []string, timeout time.Duration) (scan.ExecResult, error) {
	cur := atomic.AddInt64(&m.active, 1)
	defer atomic.AddInt64(&m.active, -1)
	for {
		peak := atomic.LoadInt64(&m.peakActive)
		if cur <= peak {
			break
		}
		if atomic.CompareAndSwapInt64(&m.peakActive, peak, cur) {
			break
		}
	}
	atomic.AddInt64(&m.totalCalls, 1)
	if m.exec == nil {
		return scan.ExecResult{Stdout: []byte("ok"), ExitCode: 0, Duration: time.Millisecond}, nil
	}
	return m.exec(ctx, target, argv, timeout)
}

// === Mock CheckEvaluator ===

type mockEvaluator struct {
	eval func(rule []byte, exec scan.ExecResult) (scan.EvalResult, error)
}

func (m *mockEvaluator) Evaluate(rule []byte, exec scan.ExecResult) (scan.EvalResult, error) {
	if m.eval == nil {
		return scan.EvalResult{Outcome: scan.OutcomePass, Reason: ""}, nil
	}
	return m.eval(rule, exec)
}

// === 테스트 셋업 ===

type harness struct {
	t            *testing.T
	store        storage.Storage
	scanSvc      scan.Service
	bus          eventbus.Bus
	executor     *mockExecutor
	evaluator    *mockEvaluator
	orch         *scanrun.Orchestrator
	tenantID     storage.TenantID
	fleetID      string
	packID       string
	robotIDs     []string
	packCheckIDs []string
}

func newHarness(t *testing.T, workerLimit int) *harness {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "scanrun.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clock.System()})
	scanSvc := scanrepo.New(scanrepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
		Audit: &auditAdapter{svc: auditSvc},
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

	mexec := &mockExecutor{}
	meval := &mockEvaluator{}

	orch := scanrun.New(scanrun.Deps{
		Scan:        scanSvc,
		Storage:     store,
		Executor:    mexec,
		Evaluator:   meval,
		Bus:         bus,
		Clock:       clock.System(),
		WorkerLimit: workerLimit,
	})

	return &harness{
		t:         t,
		store:     store,
		scanSvc:   scanSvc,
		bus:       bus,
		executor:  mexec,
		evaluator: meval,
		orch:      orch,
	}
}

// seedFleetAndPack는 한 tenant·fleet·pack을 raw INSERT합니다.
func (h *harness) seedFleetAndPack(tenantID, fleetID, packID string) {
	h.t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := h.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'test', 'desktop_free', ?)`, tenantID, now); err != nil {
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

// seedRobots는 N개의 robot을 INSERT하고 ID 슬라이스를 반환합니다.
func (h *harness) seedRobots(n int) {
	h.t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := h.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		// 단일 credential 공유.
		if _, err := tx.Exec(ctx, `INSERT INTO credentials (id, tenant_id, type, encrypted_payload, encryption_meta, rotation_due_at, created_at, updated_at, revoked_at)
VALUES ('cr_x', ?, 'password', x'00', '{}', NULL, ?, ?, NULL)`, string(h.tenantID), now, now); err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("ro_%03d", i)
			if _, err := tx.Exec(ctx, `INSERT INTO robots (id, tenant_id, fleet_id, credential_id, name, host, port, auth_type, os_distro, ros_distro, tags, role, criticality, created_at, updated_at, last_scan_at, deleted_at)
VALUES (?, ?, ?, 'cr_x', ?, ?, 22, 'password', '', '', '[]', '', 'medium', ?, ?, NULL, NULL)`,
				id, string(h.tenantID), h.fleetID, fmt.Sprintf("r%d", i), fmt.Sprintf("h%d", i), now, now); err != nil {
				return err
			}
			h.robotIDs = append(h.robotIDs, id)
		}
		return nil
	}); err != nil {
		h.t.Fatalf("seedRobots: %v", err)
	}
}

// seedChecks는 M개의 pack_check를 INSERT하고 ID 슬라이스를 반환합니다.
func (h *harness) seedChecks(m int) {
	h.t.Helper()
	if err := h.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		for i := 0; i < m; i++ {
			id := fmt.Sprintf("ck_%03d", i)
			code := fmt.Sprintf("CIS-%d", i)
			if _, err := tx.Exec(ctx, `INSERT INTO pack_checks (id, pack_id, check_id, title, severity, evaluation_rule)
VALUES (?, ?, ?, 't', 'medium', '{"op":"equals","value":"ok"}')`,
				id, h.packID, code); err != nil {
				return err
			}
			h.packCheckIDs = append(h.packCheckIDs, id)
		}
		return nil
	}); err != nil {
		h.t.Fatalf("seedChecks: %v", err)
	}
}

// startSession은 pending 상태의 ScanSession을 생성합니다.
func (h *harness) startSession(total int) string {
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

// reload는 sessionID의 현재 상태를 다시 조회합니다.
func (h *harness) reload(sessionID string) scan.ScanSession {
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

func (h *harness) listResults(sessionID string) []scan.ScanResult {
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

// makeTargets는 RobotTarget 슬라이스를 생성합니다.
func (h *harness) makeTargets() []scan.RobotTarget {
	out := make([]scan.RobotTarget, 0, len(h.robotIDs))
	for i, id := range h.robotIDs {
		out = append(out, scan.RobotTarget{
			RobotID: id, Host: fmt.Sprintf("h%d", i), Port: 22,
			AuthType: "password", CredentialID: "cr_x",
		})
	}
	return out
}

func (h *harness) makeChecks() []scan.CheckDef {
	out := make([]scan.CheckDef, 0, len(h.packCheckIDs))
	for i, id := range h.packCheckIDs {
		out = append(out, scan.CheckDef{
			PackCheckID:  id,
			Code:         fmt.Sprintf("CIS-%d", i),
			AuditCommand: []string{"echo", "ok"},
			TimeoutSec:   2,
			EvalRuleJSON: []byte(`{"op":"equals","value":"ok"}`),
		})
	}
	return out
}

// === T5 — fan-out: robots×checks 개의 결과 생성 + progress 정확 ===
func TestRunFanOutProducesResultPerRobotCheck(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 4)
	h.seedFleetAndPack("tn_T5", "fl_T5", "pk_T5")
	h.seedRobots(3)
	h.seedChecks(4)
	sessionID := h.startSession(3 * 4)

	// 모두 PASS
	h.evaluator.eval = func(_ []byte, _ scan.ExecResult) (scan.EvalResult, error) {
		return scan.EvalResult{Outcome: scan.OutcomePass}, nil
	}

	if err := h.orch.Run(context.Background(), h.tenantID, sessionID, h.makeTargets(), h.makeChecks()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := h.listResults(sessionID)
	if got, want := len(results), 12; got != want {
		t.Errorf("results = %d, want %d", got, want)
	}
	final := h.reload(sessionID)
	if final.Status != scan.StatusCompleted {
		t.Errorf("Status = %s, want completed", final.Status)
	}
	if final.Progress.Completed != 12 || final.Progress.Failed != 0 {
		t.Errorf("Progress = %+v, want {Completed:12 Failed:0}", final.Progress)
	}
	if h.executor.totalCalls != 12 {
		t.Errorf("executor calls = %d, want 12", h.executor.totalCalls)
	}
}

// === T6 — 동시 worker가 WorkerLimit을 초과하지 않음 ===
func TestRunRespectsWorkerLimit(t *testing.T) {
	t.Parallel()
	const workerLimit = 3
	h := newHarness(t, workerLimit)
	h.seedFleetAndPack("tn_T6", "fl_T6", "pk_T6")
	h.seedRobots(4)
	h.seedChecks(4) // 총 16 work item
	sessionID := h.startSession(16)

	// 각 exec가 50ms 걸림 — 동시 4 이상이면 peak에서 잡힘.
	h.executor.exec = func(ctx context.Context, _ scan.RobotTarget, _ []string, _ time.Duration) (scan.ExecResult, error) {
		select {
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			return scan.ExecResult{}, ctx.Err()
		}
		return scan.ExecResult{Stdout: []byte("ok"), ExitCode: 0, Duration: 50 * time.Millisecond}, nil
	}

	if err := h.orch.Run(context.Background(), h.tenantID, sessionID, h.makeTargets(), h.makeChecks()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if peak := atomic.LoadInt64(&h.executor.peakActive); peak > int64(workerLimit) {
		t.Errorf("peak concurrent workers = %d, want ≤ %d", peak, workerLimit)
	}
	if h.executor.totalCalls != 16 {
		t.Errorf("totalCalls = %d, want 16", h.executor.totalCalls)
	}
}

// === T9 — Cancel 호출 시 진행 중 worker는 timeout까지 대기, 다음 item skip ===
func TestRunCancelSkipsRemainingButWaitsInFlight(t *testing.T) {
	t.Parallel()
	const workerLimit = 2
	h := newHarness(t, workerLimit)
	h.seedFleetAndPack("tn_T9", "fl_T9", "pk_T9")
	h.seedRobots(10)
	h.seedChecks(2) // 총 20 work item — Cancel 시점엔 일부만 실행됨
	sessionID := h.startSession(20)

	// 각 exec 100ms — Cancel 후 진행중 2개는 완료, 나머지는 skip.
	h.executor.exec = func(ctx context.Context, _ scan.RobotTarget, _ []string, _ time.Duration) (scan.ExecResult, error) {
		select {
		case <-time.After(100 * time.Millisecond):
			return scan.ExecResult{Stdout: []byte("ok"), Duration: 100 * time.Millisecond}, nil
		case <-ctx.Done():
			// R4-5: Cancel 발생 시 진행 중 worker는 timeout 대기여야 — mock은 일단 ctx.Err로 취소 응답
			// (실제 sshpool.Executor도 ctx.Done에서 session.Close + ctx.Err 반환)
			return scan.ExecResult{}, ctx.Err()
		}
	}

	// Run을 백그라운드로 실행, 50ms 후 Cancel.
	runErr := make(chan error, 1)
	go func() {
		runErr <- h.orch.Run(context.Background(), h.tenantID, sessionID, h.makeTargets(), h.makeChecks())
	}()

	time.Sleep(50 * time.Millisecond)
	if _, err := h.orch.Cancel(context.Background(), h.tenantID, sessionID, "test cancel"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	select {
	case err := <-runErr:
		// Run은 ctx.Canceled를 반환할 수 있음.
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Run err = %v, want nil or Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return within 3s after Cancel")
	}

	final := h.reload(sessionID)
	if final.Status != scan.StatusCancelled {
		t.Errorf("Status = %s, want cancelled", final.Status)
	}
	// Cancel 후엔 totalCalls < 20이어야 함 (skip 발생).
	if h.executor.totalCalls >= 20 {
		t.Errorf("totalCalls = %d, want < 20 (some should be skipped)", h.executor.totalCalls)
	}
}

// === T10 — EventBus가 progress·completed milestone마다 publish ===
func TestRunPublishesProgressAndCompleted(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 4)
	h.seedFleetAndPack("tn_T10", "fl_T10", "pk_T10")
	h.seedRobots(2)
	h.seedChecks(3) // 총 6
	sessionID := h.startSession(6)

	// 이벤트 수집기.
	var (
		mu         sync.Mutex
		progresses []scan.ProgressEventPayload
		completed  scan.CompletedEventPayload
	)

	progSub := h.bus.Subscribe(context.Background(), scan.EventTypeProgress,
		func(_ context.Context, evt eventbus.Event) error {
			var p scan.ProgressEventPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				return err
			}
			mu.Lock()
			progresses = append(progresses, p)
			mu.Unlock()
			return nil
		})
	t.Cleanup(progSub.Cancel)

	doneCh := make(chan struct{})
	compSub := h.bus.Subscribe(context.Background(), scan.EventTypeCompleted,
		func(_ context.Context, evt eventbus.Event) error {
			mu.Lock()
			defer mu.Unlock()
			_ = json.Unmarshal(evt.Payload, &completed)
			close(doneCh)
			return nil
		})
	t.Cleanup(compSub.Cancel)

	if err := h.orch.Run(context.Background(), h.tenantID, sessionID, h.makeTargets(), h.makeChecks()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("scan.completed event not received in 2s")
	}

	// progress·completed는 별개 subscription·별개 worker goroutine.
	// completed handler가 먼저 깨어날 수 있으므로 progress 6개가 채워질 때까지 짧게 polling
	// (race mode 보호 — 운영에서 progress publish 순서·완료성은 별개 보장).
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(progresses)
		mu.Unlock()
		if n >= 6 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(progresses) != 6 {
		t.Errorf("progress events = %d, want 6", len(progresses))
	}
	// 마지막 progress의 Completed는 6.
	if len(progresses) > 0 && progresses[len(progresses)-1].Completed != 6 {
		t.Errorf("final progress.Completed = %d, want 6", progresses[len(progresses)-1].Completed)
	}
	if completed.SessionID != sessionID {
		t.Errorf("completed SessionID = %q, want %q", completed.SessionID, sessionID)
	}
	if completed.Status != "completed" {
		t.Errorf("completed Status = %q, want completed", completed.Status)
	}
	if completed.Total != 6 || completed.Completed != 6 {
		t.Errorf("completed payload progress = %+v, want Total:6 Completed:6", completed)
	}
}

// === 추가 — 빈 입력 즉시 completed ===
func TestRunEmptyInputCompletesImmediately(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 4)
	h.seedFleetAndPack("tn_E1", "fl_E1", "pk_E1")
	sessionID := h.startSession(0)

	if err := h.orch.Run(context.Background(), h.tenantID, sessionID, nil, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	final := h.reload(sessionID)
	if final.Status != scan.StatusCompleted {
		t.Errorf("Status = %s, want completed", final.Status)
	}
	if h.executor.totalCalls != 0 {
		t.Errorf("executor calls = %d, want 0", h.executor.totalCalls)
	}
}

// === 추가 — Outcome 매핑 (pass·fail·error) + Failed 진행률 ===
func TestRunRecordsMixedOutcomesAndUpdatesFailed(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 4)
	h.seedFleetAndPack("tn_M1", "fl_M1", "pk_M1")
	h.seedRobots(1)
	h.seedChecks(3)
	sessionID := h.startSession(3)

	calls := int64(0)
	h.evaluator.eval = func(_ []byte, _ scan.ExecResult) (scan.EvalResult, error) {
		idx := atomic.AddInt64(&calls, 1)
		switch idx {
		case 1:
			return scan.EvalResult{Outcome: scan.OutcomePass}, nil
		case 2:
			return scan.EvalResult{Outcome: scan.OutcomeFail, Reason: "mismatch"}, nil
		default:
			return scan.EvalResult{}, errors.New("eval failed")
		}
	}

	if err := h.orch.Run(context.Background(), h.tenantID, sessionID, h.makeTargets(), h.makeChecks()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	final := h.reload(sessionID)
	if final.Progress.Completed != 3 {
		t.Errorf("Completed = %d, want 3", final.Progress.Completed)
	}
	if final.Progress.Failed != 2 { // fail + error
		t.Errorf("Failed = %d, want 2 (fail+error)", final.Progress.Failed)
	}

	// outcomes 분포 확인.
	results := h.listResults(sessionID)
	dist := map[scan.Outcome]int{}
	for _, r := range results {
		dist[r.Outcome]++
	}
	if dist[scan.OutcomePass] != 1 || dist[scan.OutcomeFail] != 1 || dist[scan.OutcomeError] != 1 {
		t.Errorf("outcome distribution = %+v, want pass:1 fail:1 error:1", dist)
	}
}

// === 추가 — SSH error 시 Outcome=error, Reason 포함 ===
func TestRunSSHErrorRecordedAsErrorOutcome(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 4)
	h.seedFleetAndPack("tn_S1", "fl_S1", "pk_S1")
	h.seedRobots(1)
	h.seedChecks(1)
	sessionID := h.startSession(1)

	h.executor.exec = func(_ context.Context, _ scan.RobotTarget, _ []string, _ time.Duration) (scan.ExecResult, error) {
		return scan.ExecResult{}, errors.New("connection refused")
	}

	if err := h.orch.Run(context.Background(), h.tenantID, sessionID, h.makeTargets(), h.makeChecks()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	results := h.listResults(sessionID)
	if len(results) != 1 || results[0].Outcome != scan.OutcomeError {
		t.Fatalf("results = %+v, want 1 with Outcome=error", results)
	}
	if !strings.Contains(results[0].EvalReason, "connection refused") {
		t.Errorf("EvalReason = %q, want to contain 'connection refused'", results[0].EvalReason)
	}
}
