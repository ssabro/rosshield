package sqliterepo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// TestCrossTenantFuzzer는 tenant A의 ID가 tenant B의 컨텍스트에서 절대 노출되지 않음을 검증합니다.
//
// E5 Stage E `031fa05` 패턴 답습. scan 도메인에 노출된 모든 read·write 메서드를 회귀.
//
// 시나리오:
//
//	tenant A에서 Fleet+Pack+Robot+Check 셋업 → ScanSession 시작 → running 전이 → 1건 RecordResult.
//	tenant B 컨텍스트에서 모든 read·write가 tenant A의 ID에 닿으면 storage.ErrNotFound.
func TestCrossTenantFuzzer(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	const (
		tenantA, tenantB = "tn_FUZ_A", "tn_FUZ_B"
		fleetA, packA    = "fl_FUZ_A", "pk_FUZ_A"
		fleetB, packB    = "fl_FUZ_B", "pk_FUZ_B"
		robotA, ckA      = "ro_FUZ_A", "ck_FUZ_A"
		robotB, ckB      = "ro_FUZ_B", "ck_FUZ_B"
	)

	// 두 tenant·fleet·pack·robot·check 셋업.
	seedTenantFleetPack(t, store, tenantA, fleetA, packA)
	seedTenantFleetPack(t, store, tenantB, fleetB, packB)
	seedRobotAndCheck(t, store, tenantA, fleetA, packA, robotA, ckA)

	// tenantB에 다른 credential·robot·check (FK용 — id 충돌 회피 위해 별도 helper 호출 X)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec(ctx, `INSERT INTO credentials (id, tenant_id, type, encrypted_payload, encryption_meta, rotation_due_at, created_at, updated_at, revoked_at)
VALUES ('cr_FUZ_B', ?, 'password', x'00', '{}', NULL, ?, ?, NULL)`, tenantB, now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO robots (id, tenant_id, fleet_id, credential_id, name, host, port, auth_type, os_distro, ros_distro, tags, role, criticality, created_at, updated_at, last_scan_at, deleted_at)
VALUES (?, ?, ?, 'cr_FUZ_B', 'r1', 'h', 22, 'password', '', '', '[]', '', 'medium', ?, ?, NULL, NULL)`,
			robotB, tenantB, fleetB, now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pack_checks (id, pack_id, check_id, title, severity, evaluation_rule)
VALUES (?, ?, 'CIS-1.1.1.1', 'cramfs', 'medium', '{"op":"equals","value":"ok"}')`, ckB, packB); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("seed tenantB: %v", err)
	}

	// tenant A에 세션 + running + 1건 결과 셋업.
	var sessionA scan.ScanSession
	if err := store.Tx(tenantCtx(tenantA), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.StartScan(ctx, tx, sampleStartReq(fleetA, packA))
		if err != nil {
			return err
		}
		s2, err := repo.TransitionSession(ctx, tx, s.ID, scan.StatusRunning, "")
		if err != nil {
			return err
		}
		sessionA = s2
		_, err = repo.RecordResult(ctx, tx, scan.RecordResultRequest{
			SessionID: s2.ID, RobotID: robotA, CheckID: "CIS-1", PackCheckID: ckA,
			Outcome: scan.OutcomePass, ExecutedAt: time.Now().UTC(),
		})
		return err
	}); err != nil {
		t.Fatalf("setup A: %v", err)
	}

	// === tenant B 컨텍스트에서 tenant A의 ID로 모든 메서드 시도 → ErrNotFound 또는 invalid ===
	cases := []struct {
		name string
		fn   func(context.Context, storage.Tx) error
	}{
		{
			name: "GetSession(A's session) from B → ErrNotFound",
			fn: func(ctx context.Context, tx storage.Tx) error {
				_, err := repo.GetSession(ctx, tx, sessionA.ID)
				return err
			},
		},
		{
			name: "TransitionSession(A's session) from B → ErrNotFound",
			fn: func(ctx context.Context, tx storage.Tx) error {
				_, err := repo.TransitionSession(ctx, tx, sessionA.ID, scan.StatusCompleted, "")
				return err
			},
		},
		{
			name: "CancelSession(A's session) from B → ErrNotFound",
			fn: func(ctx context.Context, tx storage.Tx) error {
				_, err := repo.CancelSession(ctx, tx, sessionA.ID, "")
				return err
			},
		},
		{
			name: "RecordResult(A's session) from B → ErrNotFound",
			fn: func(ctx context.Context, tx storage.Tx) error {
				_, err := repo.RecordResult(ctx, tx, scan.RecordResultRequest{
					SessionID: sessionA.ID, RobotID: robotB, CheckID: "x", PackCheckID: ckB,
					Outcome: scan.OutcomePass, ExecutedAt: time.Now().UTC(),
				})
				return err
			},
		},
		{
			name: "StartScan with A's fleet from B → ErrFleetNotFound",
			fn: func(ctx context.Context, tx storage.Tx) error {
				_, err := repo.StartScan(ctx, tx, sampleStartReq(fleetA, packB))
				return err
			},
		},
		{
			name: "StartScan with A's pack from B → ErrPackNotFound",
			fn: func(ctx context.Context, tx storage.Tx) error {
				_, err := repo.StartScan(ctx, tx, sampleStartReq(fleetB, packA))
				return err
			},
		},
	}

	wantErrs := map[string][]error{
		"GetSession(A's session) from B → ErrNotFound":        {storage.ErrNotFound},
		"TransitionSession(A's session) from B → ErrNotFound": {storage.ErrNotFound},
		"CancelSession(A's session) from B → ErrNotFound":     {storage.ErrNotFound},
		"RecordResult(A's session) from B → ErrNotFound":      {storage.ErrNotFound},
		"StartScan with A's fleet from B → ErrFleetNotFound":  {scan.ErrFleetNotFound},
		"StartScan with A's pack from B → ErrPackNotFound":    {scan.ErrPackNotFound},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := store.Tx(tenantCtx(tenantB), func(ctx context.Context, tx storage.Tx) error {
				return tc.fn(ctx, tx)
			})
			matched := false
			for _, want := range wantErrs[tc.name] {
				if errors.Is(err, want) {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("err = %v, want one of %v", err, wantErrs[tc.name])
			}
		})
	}

	// ListSessions/ListResults는 tenant B에서 tenant A 데이터가 노출되면 안 됨.
	t.Run("ListSessions from B does not leak A", func(t *testing.T) {
		if err := store.Tx(tenantCtx(tenantB), func(ctx context.Context, tx storage.Tx) error {
			sessions, err := repo.ListSessions(ctx, tx, scan.ListSessionsFilter{})
			if err != nil {
				return err
			}
			for _, s := range sessions {
				if s.TenantID != storage.TenantID(tenantB) {
					t.Errorf("ListSessions returned session of tenant %s in tenant B context", s.TenantID)
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
	})

	t.Run("ListResults(A's session) from B returns empty", func(t *testing.T) {
		if err := store.Tx(tenantCtx(tenantB), func(ctx context.Context, tx storage.Tx) error {
			results, err := repo.ListResults(ctx, tx, sessionA.ID)
			if err != nil {
				return err
			}
			if len(results) != 0 {
				t.Errorf("ListResults returned %d rows from tenant A's session in tenant B context", len(results))
			}
			return nil
		}); err != nil {
			t.Fatalf("ListResults: %v", err)
		}
	})
}
