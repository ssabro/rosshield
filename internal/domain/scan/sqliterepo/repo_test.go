package sqliterepo_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/domain/scan/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// auditAdapter는 audit.Service를 scan.AuditEmitter로 감싸는 테스트용 구현입니다.
// (cmd/rosshield-server/bootstrap.go에 동일 패턴이 결선됨 — Stage C.)
type auditAdapter struct {
	svc audit.Service
}

func (a *auditAdapter) EmitScanStarted(ctx context.Context, tx storage.Tx, s scan.ScanSession) error {
	return a.append(ctx, tx, s, "scan.started", `{"fleetId":"`+s.FleetID+`","packId":"`+s.PackID+`"}`)
}
func (a *auditAdapter) EmitScanCompleted(ctx context.Context, tx storage.Tx, s scan.ScanSession) error {
	return a.append(ctx, tx, s, "scan.completed", `{}`)
}
func (a *auditAdapter) EmitScanFailed(ctx context.Context, tx storage.Tx, s scan.ScanSession, reason string) error {
	return a.append(ctx, tx, s, "scan.failed", `{"reason":"`+reason+`"}`)
}
func (a *auditAdapter) EmitScanCancelled(ctx context.Context, tx storage.Tx, s scan.ScanSession, reason string) error {
	return a.append(ctx, tx, s, "scan.cancelled", `{"reason":"`+reason+`"}`)
}
func (a *auditAdapter) append(ctx context.Context, tx storage.Tx, s scan.ScanSession, action, payload string) error {
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: s.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   action,
		Target:   audit.Target{Type: "scan_session", ID: s.ID},
		Payload:  []byte(payload),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

func newTestRepo(t *testing.T) (*sqliterepo.Repo, audit.Service, storage.Storage) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "scan.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clock.System()})
	repo := sqliterepo.New(sqliterepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
		Audit: &auditAdapter{svc: auditSvc},
	})
	return repo, auditSvc, store
}

// seedTenantFleetPack는 cross-FK 무결성을 만족시키기 위해 tenant·fleet·pack을 raw INSERT합니다.
// (E5 Stage C 패턴 답습 — 도메인 의존 회피로 격리 단순화.)
func seedTenantFleetPack(t *testing.T, store storage.Storage, tenantID, fleetID, packID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'test', 'desktop_free', ?)`,
			tenantID, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at)
VALUES (?, ?, 'fleet-A', '', '{}', ?, ?)`,
			fleetID, tenantID, now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO packs (
    id, tenant_id, name, version, vendor, pack_key,
    manifest_hash, signer_key_id, installed_at
) VALUES (?, ?, 'cis-ubuntu-2404', 'v1.0.0', 'CIS', 'cis-cis-ubuntu-2404-v1.0.0', x'00', 'key_test', ?)`,
			packID, tenantID, now); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("seedTenantFleetPack: %v", err)
	}
}

// seedRobotAndCheck은 RecordResult 통합 테스트에 필요한 FK 만족용 raw INSERT 헬퍼입니다.
func seedRobotAndCheck(t *testing.T, store storage.Storage, tenantID, fleetID, packID, robotID, packCheckID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		// credentials는 robot FK 만족 위해 최소만.
		if _, err := tx.Exec(ctx, `INSERT INTO credentials (
    id, tenant_id, type, encrypted_payload, encryption_meta,
    rotation_due_at, created_at, updated_at, revoked_at
) VALUES ('cr_test', ?, 'password', x'00', '{}', NULL, ?, ?, NULL)`,
			tenantID, now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO robots (
    id, tenant_id, fleet_id, credential_id, name, host, port,
    auth_type, os_distro, ros_distro, tags, role, criticality,
    created_at, updated_at, last_scan_at, deleted_at
) VALUES (?, ?, ?, 'cr_test', 'r1', 'h', 22, 'password', '', '', '[]', '', 'medium', ?, ?, NULL, NULL)`,
			robotID, tenantID, fleetID, now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pack_checks (
    id, pack_id, check_id, title, severity, evaluation_rule
) VALUES (?, ?, 'CIS-1.1.1.1', 'cramfs', 'medium', '{"op":"equals","value":"ok"}')`,
			packCheckID, packID); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("seedRobotAndCheck: %v", err)
	}
}

func tenantCtx(tenantID string) context.Context {
	return storage.WithTenantID(context.Background(), storage.TenantID(tenantID))
}

func sampleStartReq(fleetID, packID string) scan.StartScanRequest {
	return scan.StartScanRequest{
		FleetID: fleetID,
		PackID:  packID,
		Trigger: scan.TriggerManual,
		Total:   10,
	}
}

// TestStartScanCreatesPending는 Stage C 핵심 acceptance입니다.
func TestStartScanCreatesPending(t *testing.T) {
	t.Parallel()
	repo, auditSvc, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_S1", "fl_S1", "pk_S1"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	var session scan.ScanSession
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		session = s
		return err
	}); err != nil {
		t.Fatalf("StartScan: %v", err)
	}

	if session.Status != scan.StatusPending {
		t.Errorf("Status = %s, want pending", session.Status)
	}
	if session.ID == "" || len(session.ID) < 5 || session.ID[:5] != "scan_" {
		t.Errorf("ID = %q, want scan_ prefix", session.ID)
	}
	if session.Trigger != scan.TriggerManual {
		t.Errorf("Trigger = %s, want manual", session.Trigger)
	}
	if session.Progress.Total != 10 {
		t.Errorf("Progress.Total = %d, want 10", session.Progress.Total)
	}
	if session.StartedAt != nil || session.CompletedAt != nil {
		t.Errorf("StartedAt=%v CompletedAt=%v, want nil/nil for pending", session.StartedAt, session.CompletedAt)
	}
	if session.CreatedAt.IsZero() || !session.UpdatedAt.Equal(session.CreatedAt) {
		t.Errorf("timestamps wrong: created=%v updated=%v", session.CreatedAt, session.UpdatedAt)
	}

	// audit emit은 StartScan 시점에 안 함 — head seq = 0.
	var head audit.ChainHead
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		h, err := auditSvc.Head(ctx, tx, session.TenantID)
		head = h
		return err
	}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Seq != 0 {
		t.Errorf("audit head seq = %d, want 0 (StartScan does not emit)", head.Seq)
	}
}

func TestStartScanRejectsMissingFleet(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_S2", "fl_S2", "pk_S2"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.StartScan(ctx, tx, sampleStartReq("fl_NOPE", packID))
		return err
	})
	if !errors.Is(err, scan.ErrFleetNotFound) {
		t.Errorf("err = %v, want ErrFleetNotFound", err)
	}
}

func TestStartScanRejectsMissingPack(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_S3", "fl_S3", "pk_S3"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, "pk_NOPE"))
		return err
	})
	if !errors.Is(err, scan.ErrPackNotFound) {
		t.Errorf("err = %v, want ErrPackNotFound", err)
	}
}

func TestStartScanValidates(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_S4", "fl_S4", "pk_S4"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	cases := []struct {
		name    string
		mutate  func(*scan.StartScanRequest)
		wantErr error
	}{
		{"empty fleet", func(r *scan.StartScanRequest) { r.FleetID = "" }, scan.ErrSessionEmptyFleet},
		{"empty pack", func(r *scan.StartScanRequest) { r.PackID = "" }, scan.ErrSessionEmptyPack},
		{"invalid trigger", func(r *scan.StartScanRequest) { r.Trigger = "auto" }, scan.ErrSessionInvalidTrigger},
		{"negative total", func(r *scan.StartScanRequest) { r.Total = -1 }, scan.ErrSessionNegativeTotal},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := sampleStartReq(fleetID, packID)
			tc.mutate(&req)
			err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
				_, err := repo.StartScan(ctx, tx, req)
				return err
			})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestStartScanDefaultsTriggerToManual(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_S5", "fl_S5", "pk_S5"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	var session scan.ScanSession
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleStartReq(fleetID, packID)
		req.Trigger = "" // 빈 값.
		s, err := repo.StartScan(ctx, tx, req)
		session = s
		return err
	}); err != nil {
		t.Fatalf("StartScan: %v", err)
	}
	if session.Trigger != scan.TriggerManual {
		t.Errorf("Trigger = %s, want manual (default)", session.Trigger)
	}
}

func TestTransitionPendingToRunningEmitsScanStarted(t *testing.T) {
	t.Parallel()
	repo, auditSvc, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_T1", "fl_T1", "pk_T1"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	var session scan.ScanSession
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		if err != nil {
			return err
		}
		s2, err := repo.TransitionSession(ctx, tx, s.ID, scan.StatusRunning, "")
		session = s2
		return err
	}); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	if session.Status != scan.StatusRunning {
		t.Errorf("Status = %s, want running", session.Status)
	}
	if session.StartedAt == nil {
		t.Error("StartedAt should be set")
	}
	if session.CompletedAt != nil {
		t.Error("CompletedAt should be nil on running")
	}

	// 영속 후 재조회 — UPDATE 적용 검증.
	var reloaded scan.ScanSession
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.GetSession(ctx, tx, session.ID)
		reloaded = s
		return err
	}); err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if reloaded.Status != scan.StatusRunning {
		t.Errorf("reloaded Status = %s, want running", reloaded.Status)
	}
	if reloaded.StartedAt == nil {
		t.Error("reloaded StartedAt should be set")
	}

	// audit chain — scan.started 1건.
	var head audit.ChainHead
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		h, err := auditSvc.Head(ctx, tx, session.TenantID)
		head = h
		return err
	}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Seq != 1 {
		t.Errorf("audit head seq = %d, want 1 (scan.started)", head.Seq)
	}
}

func TestTransitionRunningToCompleted(t *testing.T) {
	t.Parallel()
	repo, auditSvc, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_T2", "fl_T2", "pk_T2"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	var session scan.ScanSession
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		if err != nil {
			return err
		}
		s2, err := repo.TransitionSession(ctx, tx, s.ID, scan.StatusRunning, "")
		if err != nil {
			return err
		}
		s3, err := repo.TransitionSession(ctx, tx, s2.ID, scan.StatusCompleted, "")
		session = s3
		return err
	}); err != nil {
		t.Fatalf("Transition chain: %v", err)
	}

	if session.Status != scan.StatusCompleted {
		t.Errorf("Status = %s, want completed", session.Status)
	}
	if session.StartedAt == nil || session.CompletedAt == nil {
		t.Errorf("StartedAt=%v CompletedAt=%v, want both set", session.StartedAt, session.CompletedAt)
	}

	var head audit.ChainHead
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		h, err := auditSvc.Head(ctx, tx, session.TenantID)
		head = h
		return err
	}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Seq != 2 {
		t.Errorf("audit head seq = %d, want 2 (started+completed)", head.Seq)
	}
}

func TestTransitionRejectsInvalid(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_T3", "fl_T3", "pk_T3"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		if err != nil {
			return err
		}
		// pending → completed (skipping running) — FSM 위반.
		_, err = repo.TransitionSession(ctx, tx, s.ID, scan.StatusCompleted, "")
		return err
	})
	if !errors.Is(err, scan.ErrInvalidTransition) {
		t.Errorf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestCancelFromPendingAndRunning(t *testing.T) {
	t.Parallel()
	repo, auditSvc, store := newTestRepo(t)

	cases := []struct {
		name       string
		viaRunning bool
		tenant     string
	}{
		{"from pending (R5-5)", false, "tn_C1"},
		{"from running", true, "tn_C2"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fleetID := "fl_" + tc.tenant
			packID := "pk_" + tc.tenant
			seedTenantFleetPack(t, store, tc.tenant, fleetID, packID)

			var session scan.ScanSession
			if err := store.Tx(tenantCtx(tc.tenant), func(ctx context.Context, tx storage.Tx) error {
				s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
				if err != nil {
					return err
				}
				if tc.viaRunning {
					if _, err := repo.TransitionSession(ctx, tx, s.ID, scan.StatusRunning, ""); err != nil {
						return err
					}
				}
				s2, err := repo.CancelSession(ctx, tx, s.ID, "user requested")
				session = s2
				return err
			}); err != nil {
				t.Fatalf("Cancel: %v", err)
			}

			if session.Status != scan.StatusCancelled {
				t.Errorf("Status = %s, want cancelled", session.Status)
			}
			if session.CompletedAt == nil {
				t.Error("CompletedAt should be set on cancel")
			}
			if session.FailureReason != "user requested" {
				t.Errorf("FailureReason = %q, want %q", session.FailureReason, "user requested")
			}

			var head audit.ChainHead
			if err := store.Tx(tenantCtx(tc.tenant), func(ctx context.Context, tx storage.Tx) error {
				h, err := auditSvc.Head(ctx, tx, session.TenantID)
				head = h
				return err
			}); err != nil {
				t.Fatalf("Head: %v", err)
			}
			wantSeq := int64(1)
			if tc.viaRunning {
				wantSeq = 2 // started + cancelled
			}
			if head.Seq != wantSeq {
				t.Errorf("audit head seq = %d, want %d", head.Seq, wantSeq)
			}
		})
	}
}

func TestCancelTerminalReturnsInvalidTransition(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_C3", "fl_C3", "pk_C3"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		if err != nil {
			return err
		}
		if _, err := repo.TransitionSession(ctx, tx, s.ID, scan.StatusRunning, ""); err != nil {
			return err
		}
		if _, err := repo.TransitionSession(ctx, tx, s.ID, scan.StatusCompleted, ""); err != nil {
			return err
		}
		_, err = repo.CancelSession(ctx, tx, s.ID, "too late")
		return err
	})
	if !errors.Is(err, scan.ErrInvalidTransition) {
		t.Errorf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestRecordResultUpdatesProgress(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_R1", "fl_R1", "pk_R1"
	const robotID, packCheckID = "ro_R1", "ck_R1"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)
	seedRobotAndCheck(t, store, tenantID, fleetID, packID, robotID, packCheckID)

	var session scan.ScanSession
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		if err != nil {
			return err
		}
		s2, err := repo.TransitionSession(ctx, tx, s.ID, scan.StatusRunning, "")
		session = s2
		return err
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// 1) PASS — completed=1, failed=0
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.RecordResult(ctx, tx, scan.RecordResultRequest{
			SessionID:   session.ID,
			RobotID:     robotID,
			CheckID:     "CIS-1.1.1.1",
			PackCheckID: packCheckID,
			Outcome:     scan.OutcomePass,
			ExecutedAt:  time.Now().UTC(),
		})
		return err
	}); err != nil {
		t.Fatalf("RecordResult pass: %v", err)
	}

	// 2) FAIL (다른 check) — completed=2, failed=1
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.RecordResult(ctx, tx, scan.RecordResultRequest{
			SessionID:   session.ID,
			RobotID:     robotID,
			CheckID:     "CIS-1.1.1.2",
			PackCheckID: packCheckID,
			Outcome:     scan.OutcomeFail,
			EvalReason:  "cramfs loaded",
			ExecutedAt:  time.Now().UTC(),
		})
		return err
	}); err != nil {
		t.Fatalf("RecordResult fail: %v", err)
	}

	// 3) ERROR — completed=3, failed=2
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.RecordResult(ctx, tx, scan.RecordResultRequest{
			SessionID:   session.ID,
			RobotID:     robotID,
			CheckID:     "CIS-1.1.1.3",
			PackCheckID: packCheckID,
			Outcome:     scan.OutcomeError,
			EvalReason:  "ssh timeout",
			ExecutedAt:  time.Now().UTC(),
		})
		return err
	}); err != nil {
		t.Fatalf("RecordResult error: %v", err)
	}

	var reloaded scan.ScanSession
	var results []scan.ScanResult
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.GetSession(ctx, tx, session.ID)
		if err != nil {
			return err
		}
		reloaded = s
		rs, err := repo.ListResults(ctx, tx, session.ID)
		results = rs
		return err
	}); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if reloaded.Progress.Completed != 3 {
		t.Errorf("Completed = %d, want 3", reloaded.Progress.Completed)
	}
	if reloaded.Progress.Failed != 2 {
		t.Errorf("Failed = %d, want 2 (fail+error)", reloaded.Progress.Failed)
	}
	if len(results) != 3 {
		t.Errorf("results count = %d, want 3", len(results))
	}
	// scr_ prefix 검증.
	for i, r := range results {
		if r.ID == "" || len(r.ID) < 4 || r.ID[:4] != "scr_" {
			t.Errorf("results[%d].ID = %q, want scr_ prefix", i, r.ID)
		}
	}
}

func TestRecordResultDuplicate(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_R2", "fl_R2", "pk_R2"
	const robotID, packCheckID = "ro_R2", "ck_R2"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)
	seedRobotAndCheck(t, store, tenantID, fleetID, packID, robotID, packCheckID)

	var sessionID string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		if err != nil {
			return err
		}
		if _, err := repo.TransitionSession(ctx, tx, s.ID, scan.StatusRunning, ""); err != nil {
			return err
		}
		sessionID = s.ID
		_, err = repo.RecordResult(ctx, tx, scan.RecordResultRequest{
			SessionID: sessionID, RobotID: robotID, CheckID: "CIS-1", PackCheckID: packCheckID,
			Outcome: scan.OutcomePass, ExecutedAt: time.Now().UTC(),
		})
		return err
	}); err != nil {
		t.Fatalf("first RecordResult: %v", err)
	}

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.RecordResult(ctx, tx, scan.RecordResultRequest{
			SessionID: sessionID, RobotID: robotID, CheckID: "CIS-1", PackCheckID: packCheckID,
			Outcome: scan.OutcomeFail, ExecutedAt: time.Now().UTC(),
		})
		return err
	})
	if !errors.Is(err, scan.ErrResultDuplicate) {
		t.Errorf("err = %v, want ErrResultDuplicate", err)
	}
}

func TestRecordResultRequiresRunning(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_R3", "fl_R3", "pk_R3"
	const robotID, packCheckID = "ro_R3", "ck_R3"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)
	seedRobotAndCheck(t, store, tenantID, fleetID, packID, robotID, packCheckID)

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		if err != nil {
			return err
		}
		// pending인 상태로 RecordResult 시도.
		_, err = repo.RecordResult(ctx, tx, scan.RecordResultRequest{
			SessionID: s.ID, RobotID: robotID, CheckID: "CIS-1", PackCheckID: packCheckID,
			Outcome: scan.OutcomePass, ExecutedAt: time.Now().UTC(),
		})
		return err
	})
	if !errors.Is(err, scan.ErrSessionNotRunning) {
		t.Errorf("err = %v, want ErrSessionNotRunning", err)
	}
}

func TestRecordResultValidates(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_R4", "fl_R4", "pk_R4"
	const robotID, packCheckID = "ro_R4", "ck_R4"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)
	seedRobotAndCheck(t, store, tenantID, fleetID, packID, robotID, packCheckID)

	var sessionID string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		if err != nil {
			return err
		}
		if _, err := repo.TransitionSession(ctx, tx, s.ID, scan.StatusRunning, ""); err != nil {
			return err
		}
		sessionID = s.ID
		return nil
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*scan.RecordResultRequest)
		wantErr error
	}{
		{"empty robot", func(r *scan.RecordResultRequest) { r.RobotID = "" }, scan.ErrResultEmptyRobot},
		{"empty check", func(r *scan.RecordResultRequest) { r.CheckID = "" }, scan.ErrResultEmptyCheck},
		{"empty packcheck", func(r *scan.RecordResultRequest) { r.PackCheckID = "" }, scan.ErrResultEmptyPackCheck},
		{"invalid outcome", func(r *scan.RecordResultRequest) { r.Outcome = "PASS" }, scan.ErrResultInvalidOutcome},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := scan.RecordResultRequest{
				SessionID: sessionID, RobotID: robotID, CheckID: "CIS-X",
				PackCheckID: packCheckID, Outcome: scan.OutcomePass, ExecutedAt: time.Now().UTC(),
			}
			tc.mutate(&req)
			err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
				_, err := repo.RecordResult(ctx, tx, req)
				return err
			})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestListSessionsFilter(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_L1", "fl_L1", "pk_L1"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	// 추가 fleet도 하나.
	const fleet2 = "fl_L1B"
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at) VALUES (?, ?, 'fleet-B', '', '{}', ?, ?)`,
			fleet2, tenantID, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatalf("seed fleet2: %v", err)
	}

	// fleet1 × 2 세션 (둘째는 첫째를 cancelled로 보낸 뒤 시작 — fleet 동시 limit 회피),
	// fleet2 × 1 세션.
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s1, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		if err != nil {
			return err
		}
		if _, err := repo.CancelSession(ctx, tx, s1.ID, "test rotation"); err != nil {
			return err
		}
		if _, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID)); err != nil {
			return err
		}
		_, err = repo.StartScan(ctx, tx, sampleStartReq(fleet2, packID))
		return err
	}); err != nil {
		t.Fatalf("StartScan loop: %v", err)
	}

	var allCount, fleet1Count, fleet2Count int
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		all, err := repo.ListSessions(ctx, tx, scan.ListSessionsFilter{})
		if err != nil {
			return err
		}
		allCount = len(all)
		l1, err := repo.ListSessions(ctx, tx, scan.ListSessionsFilter{FleetID: fleetID})
		if err != nil {
			return err
		}
		fleet1Count = len(l1)
		l2, err := repo.ListSessions(ctx, tx, scan.ListSessionsFilter{FleetID: fleet2})
		if err != nil {
			return err
		}
		fleet2Count = len(l2)
		return nil
	}); err != nil {
		t.Fatalf("List: %v", err)
	}

	if allCount != 3 {
		t.Errorf("all count = %d, want 3", allCount)
	}
	if fleet1Count != 2 {
		t.Errorf("fleet1 count = %d, want 2", fleet1Count)
	}
	if fleet2Count != 1 {
		t.Errorf("fleet2 count = %d, want 1", fleet2Count)
	}
}

func TestListSessionsByStatus(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_L2", "fl_L2", "pk_L2"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	// 추가 fleet 하나 — fleet 동시 limit 때문에 fleet 별로 active 1건만 가능.
	const fleet2 = "fl_L2B"
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at) VALUES (?, ?, 'fleet-B', '', '{}', ?, ?)`,
			fleet2, tenantID, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatalf("seed fleet2: %v", err)
	}

	// fleet1: pending, fleet2: running (둘 다 동시에 활성 가능 — 다른 fleet).
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		if _, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID)); err != nil {
			return err
		}
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleet2, packID))
		if err != nil {
			return err
		}
		_, err = repo.TransitionSession(ctx, tx, s.ID, scan.StatusRunning, "")
		return err
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var pending, running []scan.ScanSession
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		p, err := repo.ListSessions(ctx, tx, scan.ListSessionsFilter{Status: scan.StatusPending})
		if err != nil {
			return err
		}
		pending = p
		r, err := repo.ListSessions(ctx, tx, scan.ListSessionsFilter{Status: scan.StatusRunning})
		running = r
		return err
	}); err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(pending) != 1 {
		t.Errorf("pending = %d, want 1", len(pending))
	}
	if len(running) != 1 {
		t.Errorf("running = %d, want 1", len(running))
	}
}

func TestStartScanRejectsDuplicateActiveSessionOnSameFleet(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_FA", "fl_FA", "pk_FA"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	// 첫 StartScan은 통과.
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		return err
	}); err != nil {
		t.Fatalf("first StartScan: %v", err)
	}

	// 두 번째 — 같은 fleet, pending 살아있음 → ErrFleetActiveScanExists.
	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		return err
	})
	if !errors.Is(err, scan.ErrFleetActiveScanExists) {
		t.Errorf("err = %v, want ErrFleetActiveScanExists", err)
	}
}

func TestStartScanAllowsAfterCancellingActiveSession(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID, packID = "tn_FB", "fl_FB", "pk_FB"
	seedTenantFleetPack(t, store, tenantID, fleetID, packID)

	// 첫 → cancel → 두 번째 OK.
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		if err != nil {
			return err
		}
		if _, err := repo.CancelSession(ctx, tx, s.ID, "freeing slot"); err != nil {
			return err
		}
		_, err = repo.StartScan(ctx, tx, sampleStartReq(fleetID, packID))
		return err
	}); err != nil {
		t.Fatalf("expected success after cancel: %v", err)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_L3"
	seedTenantFleetPack(t, store, tenantID, "fl_L3", "pk_L3")

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GetSession(ctx, tx, "scan_NOPE")
		return err
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// system pack(tenant_id='system')은 cross-tenant 공유 가능 — StartScan이 호출 tenant 또는 'system' 둘 다 허용.
func TestStartScanAcceptsSystemPack(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID, fleetID = "tn_SYS", "fl_SYS"
	const systemPack = "pk_SYS"

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'test', 'desktop_free', ?)`,
			tenantID, now); err != nil {
			return err
		}
		// 'system' tenant도 미리 등록 (FK 만족).
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES ('system', 'system', 'desktop_free', ?)`, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at) VALUES (?, ?, 'fleet', '', '{}', ?, ?)`,
			fleetID, tenantID, now, now); err != nil {
			return err
		}
		// system pack — tenant_id='system'.
		_, err := tx.Exec(ctx, `INSERT INTO packs (
    id, tenant_id, name, version, vendor, pack_key,
    manifest_hash, signer_key_id, installed_at
) VALUES (?, 'system', 'cis', 'v1.0.0', 'CIS', 'cis-cis-v1.0.0', x'00', 'key_test', ?)`,
			systemPack, now)
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var session scan.ScanSession
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetID, systemPack))
		session = s
		return err
	}); err != nil {
		t.Fatalf("StartScan with system pack: %v", err)
	}
	if session.PackID != systemPack {
		t.Errorf("PackID = %s, want %s", session.PackID, systemPack)
	}
}
