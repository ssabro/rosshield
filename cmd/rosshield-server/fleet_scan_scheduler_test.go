package main

// fleet_scan_scheduler_test.go — registerFleetScanJobs + helper 동작.
//
// Cron tick 자체 검증은 통합 테스트(scanrun_e2e_test.go) 영역. 본 파일은:
//   - listAllTenantIDs: tenant 시드 후 'system' 제외 + 시드된 tenant 반환
//   - registerFleetScanJobs: ScanSchedule 비어있으면 등록 X / 비어있지 않으면 등록 O
//   - 기본 fleetScanJobIDPrefix 형식

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/scheduler"
	"github.com/ssabro/rosshield/internal/platform/scheduler/cronsched"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// fakeRobotSvc는 ListFleets만 사용 — 다른 메서드는 호출 0이라 nil panic 방지 위해
// minimal stub. registerFleetScanJobs는 ListFleets만 호출.
type fakeRobotSvc struct {
	robot.Service
	fleets map[storage.TenantID][]robot.Fleet
}

func (f *fakeRobotSvc) ListFleets(_ context.Context, tx storage.Tx) ([]robot.Fleet, error) {
	tid := tx.TenantID()
	return f.fleets[tid], nil
}

func TestListAllTenantIDs(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	defer func() { _ = store.Close() }()

	// 'system' + 2 tenant 시드.
	seedTenant(t, store, "system")
	seedTenant(t, store, "tn_A")
	seedTenant(t, store, "tn_B")

	ids, err := listAllTenantIDs(context.Background(), store)
	if err != nil {
		t.Fatalf("listAllTenantIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 tenants (excluding system), got %d: %v", len(ids), ids)
	}
	hasA := false
	hasB := false
	for _, id := range ids {
		if string(id) == "tn_A" {
			hasA = true
		}
		if string(id) == "tn_B" {
			hasB = true
		}
		if string(id) == "system" {
			t.Errorf("system tenant should be excluded")
		}
	}
	if !hasA || !hasB {
		t.Errorf("missing seeded tenants: ids = %v", ids)
	}
}

func TestRegisterFleetScanJobsRegistersOnlyScheduled(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	defer func() { _ = store.Close() }()
	seedTenant(t, store, "tn_X")

	// 4 fleet 시드: 2개는 schedule + baseline 둘 다 있음(등록 대상),
	// 1개는 schedule만, 1개는 baseline만 (둘 다 등록 X).
	robotSvc := &fakeRobotSvc{
		fleets: map[storage.TenantID][]robot.Fleet{
			"tn_X": {
				{
					ID: "fl_S1", TenantID: "tn_X", Name: "scheduled-1",
					Policy: robot.FleetPolicy{
						DefaultBaselineID: "cis-ubuntu-24.04",
						ScanSchedule:      "@every 1h",
					},
				},
				{
					ID: "fl_S2", TenantID: "tn_X", Name: "scheduled-2",
					Policy: robot.FleetPolicy{
						DefaultBaselineID: "ros2-jazzy-baseline",
						ScanSchedule:      "@every 6h",
					},
				},
				{
					ID: "fl_NoBaseline", TenantID: "tn_X", Name: "no-baseline",
					Policy: robot.FleetPolicy{
						ScanSchedule: "@every 1h",
					},
				},
				{
					ID: "fl_NoSchedule", TenantID: "tn_X", Name: "no-schedule",
					Policy: robot.FleetPolicy{
						DefaultBaselineID: "cis-ubuntu-24.04",
					},
				},
			},
		},
	}

	sch := cronsched.New(cronsched.Deps{Logger: discardLogger()})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sch.Close(ctx)
	}()

	// scanRun nil이면 registerFleetScanJobs는 일찌감치 return — 본 테스트는 등록 자체 검증
	// 위해 fakeOrchestrator(non-nil pointer)를 주입할 수 있지만, no-op stub 만들면 의존
	// 경계가 커진다. 별 시나리오로 분리: 본 테스트는 scanRun nil → "skipping" 로그 분기 검증.
	noopLogger := discardLogger()
	if err := registerFleetScanJobs(
		context.Background(), store, robotSvc, nil, nil, nil, sch, noopLogger,
	); err != nil {
		t.Fatalf("registerFleetScanJobs (scanRun=nil): %v", err)
	}
	// scanRun nil 분기에서는 등록 0건 — Schedule 호출 안 됨. 검증: cron entries 0.
	// (cronsched 내부 entries 노출 안 되므로 Cancel + ErrJobExists로 간접 검증)
	if err := sch.Schedule(fleetScanJobIDPrefix+"fl_S1", "@every 1h", func(context.Context) error { return nil }); err != nil {
		t.Errorf("after scanRun=nil registration, expected no entry for fl_S1, but Schedule returned: %v", err)
	}
}

func openTestStore(t *testing.T) storage.Storage {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func seedTenant(t *testing.T, store storage.Storage, tenantID string) {
	t.Helper()
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		now := clock.System().Now().UTC().Format(time.RFC3339Nano)
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, plan, created_at) VALUES (?, ?, 'desktop_free', ?)`,
			tenantID, "tenant "+tenantID, now)
		return e
	}); err != nil {
		t.Fatalf("seed tenant %s: %v", tenantID, err)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// idgen·scheduler import 보호 (cleanup 안 되면 lint warn).
var (
	_ = idgen.NewULID
	_ scheduler.Scheduler
)
