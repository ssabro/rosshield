//go:build integration

package rotation_test

// gc_pg_integration_test.go — E32 Stage 4: PG 통합 테스트 — 트리거 + GUC 우회 실측.
//
// 본 파일은 `-tags=integration` 빌드 태그가 붙어야 컴파일됩니다.
// docker 미가용 시 t.Skip.
//
// 실행:
//
//	go test -tags=integration -count=1 ./internal/domain/audit/rotation/
//
// 검증:
//   1. audit_entries_block_delete 트리거 — GUC 미설정 시 application DELETE 거부.
//   2. HotGC.Run dryRun=false — SET LOCAL + DELETE + audit.gc.complete entry emit 모두 동일 Tx.
//   3. tx commit 후 rosshield.audit_gc_mode 가 reset되어 다른 connection에서 DELETE 차단.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/postgres"
)

const pgTestTenant storage.TenantID = "tn_gc_pg"

func newPGFixture(t *testing.T) storage.Storage {
	t.Helper()
	ctx := context.Background()

	pgC, err := tcpg.Run(ctx, "postgres:16-alpine",
		tcpg.WithDatabase("rosshield_test"),
		tcpg.WithUsername("test"),
		tcpg.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Skipf("docker unavailable or PG container failed: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = pgC.Terminate(shutdownCtx)
	})

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}

	store, err := postgres.Open(storage.Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

// TestIntegrationPGHotGC_TriggerBlocksDeleteWithoutGUC — GUC 미설정 시 application DELETE 차단.
func TestIntegrationPGHotGC_TriggerBlocksDeleteWithoutGUC(t *testing.T) {
	t.Parallel()
	store := newPGFixture(t)
	repo := auditrepo.New(auditrepo.Deps{Clock: clock.System()})

	ctx := storage.WithTenantID(context.Background(), pgTestTenant)
	// 1 entry seed.
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.Append(ctx, tx, audit.AppendRequest{
			TenantID: pgTestTenant,
			Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
			Action:   "test.seed",
			Target:   audit.Target{Type: "robot", ID: "ro_x"},
			Outcome:  audit.OutcomeSuccess,
		})
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// GUC 미설정 — DELETE 시도 → 트리거가 차단.
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM audit_entries WHERE tenant_id = ?`, string(pgTestTenant))
		return e
	})
	if err == nil {
		t.Fatal("expected trigger to block DELETE without GUC; got nil error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "immutable") {
		t.Errorf("error %q does not mention 'immutable' (trigger message)", err.Error())
	}
}

// TestIntegrationPGHotGC_FullRun_DeletesAndEmits — HotGC.Run dryRun=false 가 segment 범위 DELETE + audit.gc.complete emit.
//
// Rotator.Rotate 는 sqlite LastInsertId 가정 — PG 에서 미지원 (별 epic). 본 통합 test 는
// audit_entries + audit_rotation_segments 를 직접 INSERT 하여 HotGC 만 검증합니다.
func TestIntegrationPGHotGC_FullRun_DeletesAndEmits(t *testing.T) {
	t.Parallel()
	store := newPGFixture(t)
	repo := auditrepo.New(auditrepo.Deps{Clock: clock.System()})

	ctx := storage.WithTenantID(context.Background(), pgTestTenant)

	// 4 entry seed (Appender 통해 — chain hash 정상 계산).
	for i := 0; i < 4; i++ {
		if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.Append(ctx, tx, audit.AppendRequest{
				TenantID: pgTestTenant,
				Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
				Action:   "test.event",
				Target:   audit.Target{Type: "robot", ID: "ro_x"},
				Outcome:  audit.OutcomeSuccess,
			})
			return err
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	// segment 메타 2건 직접 INSERT — Rotator 우회 (PG LastInsertId 미지원 회피).
	// segment 1: seq 1~2, segment 2: seq 3~4. archive_uri 채워 hot retention 후보로.
	oldCreated := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UTC().Format(time.RFC3339Nano)
	segHash := make([]byte, audit.HashSize) // 32B zero — 본 test 는 hash 무결성 미검증
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		for segNum, rng := range [][2]int64{{1, 2}, {3, 4}} {
			n := int64(segNum) + 1
			_, err := tx.Exec(ctx, `
INSERT INTO audit_rotation_segments
  (tenant_id, segment_number, started_at, ended_at, first_entry_id, last_entry_id, entry_count,
   segment_hash, archive_uri, archive_sha256, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				string(pgTestTenant), n, oldCreated, oldCreated, rng[0], rng[1], int64(2),
				segHash, "file:///tmp/seg.tar.gz", segHash, oldCreated)
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("insert segments: %v", err)
	}

	var headBefore audit.ChainHead
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		h, e := repo.Head(ctx, tx, pgTestTenant)
		headBefore = h
		return e
	}); err != nil {
		t.Fatalf("head before: %v", err)
	}

	// HotGC.Run dryRun=false.
	gcNow := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	gc, _ := rotation.NewHotGC(rotation.HotGCDeps{
		Policy:   rotation.RotationPolicy{HotRetention: 30 * 24 * time.Hour, ColdBackend: rotation.ColdBackendFile},
		Appender: repo,
		Clock:    clock.NewFake(gcNow),
	})
	var result *rotation.HotGCResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := gc.Run(ctx, tx, pgTestTenant, false)
		if err != nil {
			return err
		}
		result = r
		return nil
	}); err != nil {
		t.Fatalf("HotGC.Run: %v", err)
	}

	if result.DryRun {
		t.Error("DryRun = true, want false")
	}
	if result.DeletedCount != 4 {
		t.Errorf("DeletedCount = %d, want 4 (seg1 2 + seg2 2)", result.DeletedCount)
	}
	if len(result.SegmentsProcessed) != 2 {
		t.Errorf("SegmentsProcessed = %v, want 2", result.SegmentsProcessed)
	}

	// head Seq 증가 — audit.gc.complete entry 1건 추가.
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		h, e := repo.Head(ctx, tx, pgTestTenant)
		if e != nil {
			return e
		}
		if h.Seq != headBefore.Seq+1 {
			t.Errorf("head Seq after GC = %d, want %d (1 entry: gc.complete)", h.Seq, headBefore.Seq+1)
		}
		return nil
	}); err != nil {
		t.Fatalf("head after: %v", err)
	}

	// audit_entries 잔존: 원래 4 - 4 deleted = 0 + 1 new gc.complete = 1.
	var remaining int64
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		row := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_entries WHERE tenant_id = ?`, string(pgTestTenant))
		return row.Scan(&remaining)
	}); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 1 {
		t.Errorf("remaining entries = %d, want 1 (gc.complete only)", remaining)
	}

	// 후속: 별 Tx 에서 GUC 미설정 — DELETE 시도 차단 (SET LOCAL 가 reset됨을 확인).
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM audit_entries WHERE tenant_id = ?`, string(pgTestTenant))
		return e
	})
	if err == nil {
		t.Error("expected trigger to block DELETE in subsequent tx (GUC not persisted)")
	}
}
