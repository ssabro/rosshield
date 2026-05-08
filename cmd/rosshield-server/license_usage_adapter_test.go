package main

// license_usage_adapter_test.go — E24-D: licenseUsageAdapter 단위 테스트.
//
// 시나리오:
//   - CurrentRobots: 활성 robot N → 정확한 카운트, soft-deleted 제외, 다른 tenant 격리.
//   - ScansToday: 오늘 scan만 카운트, 어제 created_at은 제외, tenant 격리.
//   - LLMTokensToday: 오늘 advisor_turns 의 input+output 합, 어제 turn은 제외, tenant 격리.
//   - 빈 tenantID → error.
//
// fixture: Platform Bootstrap → tenant 시드 → 도메인 service / 직접 SQL로 데이터 시드 →
// 어댑터 호출 → 기대값 비교.

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/tenant"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// licUsageFixture는 어댑터 테스트의 의존성 묶음입니다.
type licUsageFixture struct {
	platform   *Platform
	tenantID   storage.TenantID
	otherID    storage.TenantID
	fleetID    string
	otherFleet string
	adminID    string // tenantID 측 admin user ID (advisor_turns FK 만족용)
	otherAdmin string // otherID 측 admin user ID
}

// newLicUsageFixture는 Platform + 두 tenant + 각 fleet 1개를 시드합니다.
func newLicUsageFixture(t *testing.T) *licUsageFixture {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		DataDir: dir,
		Logger:  slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	p, err := Bootstrap(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	})

	// tenant 두 개 — 격리 검증용.
	ctx := context.Background()
	var t1, t2 tenant.CreateResult
	err = p.Storage.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		r1, e := p.Tenant.Create(ctx, tx, tenant.CreateRequest{
			Name:             "Acme",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       "acme@example.com",
			AdminPassword:    "verylongpassword123",
			AdminDisplayName: "Acme Admin",
		})
		if e != nil {
			return e
		}
		t1 = r1
		r2, e := p.Tenant.Create(ctx, tx, tenant.CreateRequest{
			Name:             "Globex",
			Plan:             tenant.PlanDesktopFree,
			AdminEmail:       "globex@example.com",
			AdminPassword:    "verylongpassword123",
			AdminDisplayName: "Globex Admin",
		})
		if e != nil {
			return e
		}
		t2 = r2
		return nil
	})
	if err != nil {
		t.Fatalf("seed tenants: %v", err)
	}

	// 각 tenant 안 fleet 1개.
	var fleetA, fleetB string
	if err := p.Storage.Tx(storage.WithTenantID(ctx, t1.Tenant.ID),
		func(ctx context.Context, tx storage.Tx) error {
			fl, e := p.Robot.CreateFleet(ctx, tx, robot.CreateFleetRequest{Name: "fleet-a"})
			if e != nil {
				return e
			}
			fleetA = fl.ID
			return nil
		}); err != nil {
		t.Fatalf("seed fleet a: %v", err)
	}
	if err := p.Storage.Tx(storage.WithTenantID(ctx, t2.Tenant.ID),
		func(ctx context.Context, tx storage.Tx) error {
			fl, e := p.Robot.CreateFleet(ctx, tx, robot.CreateFleetRequest{Name: "fleet-b"})
			if e != nil {
				return e
			}
			fleetB = fl.ID
			return nil
		}); err != nil {
		t.Fatalf("seed fleet b: %v", err)
	}

	return &licUsageFixture{
		platform:   p,
		tenantID:   t1.Tenant.ID,
		otherID:    t2.Tenant.ID,
		fleetID:    fleetA,
		otherFleet: fleetB,
		adminID:    t1.Admin.ID,
		otherAdmin: t2.Admin.ID,
	}
}

func (f *licUsageFixture) seedRobot(t *testing.T, tenantID storage.TenantID, fleetID, name, host string) string {
	t.Helper()
	ctx := storage.WithTenantID(context.Background(), tenantID)
	var rid string
	if err := f.platform.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		res, e := f.platform.Robot.CreateRobot(ctx, tx, robot.CreateRobotRequest{
			FleetID:  fleetID,
			Name:     name,
			Host:     host,
			Port:     22,
			AuthType: robot.AuthTypePassword,
			Material: robot.CredentialMaterial{
				Type:     robot.CredentialTypePassword,
				Username: "ros",
				Password: "p",
			},
		})
		if e != nil {
			return e
		}
		rid = res.Robot.ID
		return nil
	}); err != nil {
		t.Fatalf("seed robot: %v", err)
	}
	return rid
}

func (f *licUsageFixture) deleteRobot(t *testing.T, tenantID storage.TenantID, robotID string) {
	t.Helper()
	ctx := storage.WithTenantID(context.Background(), tenantID)
	if err := f.platform.Storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		return f.platform.Robot.DeleteRobot(ctx, tx, robotID)
	}); err != nil {
		t.Fatalf("delete robot: %v", err)
	}
}

func (f *licUsageFixture) seedPack(t *testing.T, tenantID storage.TenantID, packID string) string {
	t.Helper()
	if err := f.platform.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		_, e := tx.Exec(ctx,
			`INSERT INTO packs (id, tenant_id, name, version, vendor, pack_key, manifest_hash, signer_key_id, installed_at)
VALUES (?, ?, 'cis', '1.0', 'CIS', ?, x'00', 'key_test', ?)`,
			packID, string(tenantID), packID+"-key", now)
		return e
	}); err != nil {
		t.Fatalf("seed pack: %v", err)
	}
	return packID
}

// seedScanSession은 scan_session을 직접 INSERT합니다 (created_at 시점 제어를 위해 raw SQL).
//
// scan.Service.StartScan은 Clock.Now를 박아 created_at을 결정 — 어제 시점 시드는 직접 SQL.
func (f *licUsageFixture) seedScanSession(t *testing.T, tenantID storage.TenantID, fleetID, packID, sessionID string, createdAt time.Time) {
	t.Helper()
	if err := f.platform.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		ts := createdAt.UTC().Format(time.RFC3339Nano)
		_, e := tx.Exec(ctx,
			`INSERT INTO scan_sessions (
				id, tenant_id, fleet_id, pack_id, trigger, status,
				progress_total, progress_completed, progress_failed,
				failure_reason, created_at, updated_at
			) VALUES (?, ?, ?, ?, 'manual', 'pending', 0, 0, 0, '', ?, ?)`,
			sessionID, string(tenantID), fleetID, packID, ts, ts)
		return e
	}); err != nil {
		t.Fatalf("seed scan session: %v", err)
	}
}

// seedAdvisorTurn은 advisor_conversation + advisor_turn을 직접 INSERT (토큰·시점 제어).
//
// user_id는 fixture가 만든 admin 사용 — users FK 만족.
func (f *licUsageFixture) seedAdvisorTurn(t *testing.T, tenantID storage.TenantID, convID, turnID string, inTok, outTok int, createdAt time.Time) {
	t.Helper()
	userID := f.adminID
	if tenantID == f.otherID {
		userID = f.otherAdmin
	}
	if err := f.platform.Storage.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		ts := createdAt.UTC().Format(time.RFC3339Nano)
		// conversation 시드 (idempotent — 이미 있으면 INSERT OR IGNORE).
		if _, e := tx.Exec(ctx,
			`INSERT OR IGNORE INTO advisor_conversations (id, tenant_id, user_id, title, created_at, updated_at)
			 VALUES (?, ?, ?, '', ?, ?)`,
			convID, string(tenantID), userID, ts, ts); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO advisor_turns (
				id, conversation_id, tenant_id, role, content, sequence,
				llm_provider, llm_model, input_tokens, output_tokens, cost_usd, created_at
			) VALUES (?, ?, ?, 'assistant', '', 0, 'noop', '', ?, ?, 0, ?)`,
			turnID, convID, string(tenantID), inTok, outTok, ts)
		return e
	}); err != nil {
		t.Fatalf("seed advisor turn: %v", err)
	}
}

// === CurrentRobots ===

func TestLicenseUsageCurrentRobotsCountsActiveOnly(t *testing.T) {
	t.Parallel()
	f := newLicUsageFixture(t)

	r1 := f.seedRobot(t, f.tenantID, f.fleetID, "rob-1", "10.0.0.1")
	_ = f.seedRobot(t, f.tenantID, f.fleetID, "rob-2", "10.0.0.2")
	r3 := f.seedRobot(t, f.tenantID, f.fleetID, "rob-3", "10.0.0.3")
	f.deleteRobot(t, f.tenantID, r3) // soft delete → 카운트 제외.
	_ = r1

	a := newLicenseUsageAdapter(f.platform.Storage, f.platform.Clock)
	got, err := a.CurrentRobots(context.Background(), string(f.tenantID))
	if err != nil {
		t.Fatalf("CurrentRobots: %v", err)
	}
	if got != 2 {
		t.Errorf("CurrentRobots = %d, want 2 (3 created, 1 soft-deleted)", got)
	}
}

func TestLicenseUsageCurrentRobotsTenantIsolated(t *testing.T) {
	t.Parallel()
	f := newLicUsageFixture(t)

	_ = f.seedRobot(t, f.tenantID, f.fleetID, "rob-a", "10.0.0.10")
	_ = f.seedRobot(t, f.otherID, f.otherFleet, "rob-b1", "10.0.0.11")
	_ = f.seedRobot(t, f.otherID, f.otherFleet, "rob-b2", "10.0.0.12")

	a := newLicenseUsageAdapter(f.platform.Storage, f.platform.Clock)

	gotA, err := a.CurrentRobots(context.Background(), string(f.tenantID))
	if err != nil {
		t.Fatalf("CurrentRobots tenant A: %v", err)
	}
	if gotA != 1 {
		t.Errorf("tenant A = %d, want 1", gotA)
	}
	gotB, err := a.CurrentRobots(context.Background(), string(f.otherID))
	if err != nil {
		t.Fatalf("CurrentRobots tenant B: %v", err)
	}
	if gotB != 2 {
		t.Errorf("tenant B = %d, want 2", gotB)
	}
}

func TestLicenseUsageCurrentRobotsRejectsEmptyTenantID(t *testing.T) {
	t.Parallel()
	f := newLicUsageFixture(t)

	a := newLicenseUsageAdapter(f.platform.Storage, f.platform.Clock)
	_, err := a.CurrentRobots(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty tenantID")
	}
}

// === ScansToday ===

func TestLicenseUsageScansTodayCountsTodayOnly(t *testing.T) {
	t.Parallel()
	f := newLicUsageFixture(t)

	packID := f.seedPack(t, f.tenantID, "pk_LIC1")

	// now가 UTC 자정 직후일 수 있으므로 today/yesterday를 자정 기준으로 명시 계산.
	// 결정성·day-boundary 안전.
	now := f.platform.Clock.Now()
	startToday := todayStartUTC(now)
	yesterday := startToday.Add(-3 * time.Hour) // 어제 21:00 UTC
	today := startToday.Add(1 * time.Hour)      // 오늘 01:00 UTC (자정 직후 안전)

	f.seedScanSession(t, f.tenantID, f.fleetID, packID, "scan_TODAY1", today)
	f.seedScanSession(t, f.tenantID, f.fleetID, packID, "scan_TODAY2", today.Add(30*time.Minute))
	f.seedScanSession(t, f.tenantID, f.fleetID, packID, "scan_OLD", yesterday)

	a := newLicenseUsageAdapter(f.platform.Storage, f.platform.Clock)
	got, err := a.ScansToday(context.Background(), string(f.tenantID))
	if err != nil {
		t.Fatalf("ScansToday: %v", err)
	}
	if got != 2 {
		t.Errorf("ScansToday = %d, want 2 (3 created, 1 yesterday)", got)
	}
}

func TestLicenseUsageScansTodayTenantIsolated(t *testing.T) {
	t.Parallel()
	f := newLicUsageFixture(t)

	packA := f.seedPack(t, f.tenantID, "pk_A")
	packB := f.seedPack(t, f.otherID, "pk_B")
	now := f.platform.Clock.Now()

	f.seedScanSession(t, f.tenantID, f.fleetID, packA, "scan_A1", now)
	f.seedScanSession(t, f.otherID, f.otherFleet, packB, "scan_B1", now)
	f.seedScanSession(t, f.otherID, f.otherFleet, packB, "scan_B2", now)

	a := newLicenseUsageAdapter(f.platform.Storage, f.platform.Clock)
	gotA, _ := a.ScansToday(context.Background(), string(f.tenantID))
	gotB, _ := a.ScansToday(context.Background(), string(f.otherID))
	if gotA != 1 {
		t.Errorf("tenant A = %d, want 1", gotA)
	}
	if gotB != 2 {
		t.Errorf("tenant B = %d, want 2", gotB)
	}
}

func TestLicenseUsageScansTodayEmptyZero(t *testing.T) {
	t.Parallel()
	f := newLicUsageFixture(t)

	a := newLicenseUsageAdapter(f.platform.Storage, f.platform.Clock)
	got, err := a.ScansToday(context.Background(), string(f.tenantID))
	if err != nil {
		t.Fatalf("ScansToday: %v", err)
	}
	if got != 0 {
		t.Errorf("ScansToday = %d, want 0 (no scans)", got)
	}
}

// === LLMTokensToday ===

func TestLicenseUsageLLMTokensTodaySumsTodayOnly(t *testing.T) {
	t.Parallel()
	f := newLicUsageFixture(t)

	// day-boundary 안전 (now가 자정 직후일 수 있어 자정 기준으로 명시 계산).
	now := f.platform.Clock.Now()
	startToday := todayStartUTC(now)
	yesterday := startToday.Add(-3 * time.Hour) // 어제 21:00 UTC
	today := startToday.Add(1 * time.Hour)      // 오늘 01:00 UTC

	f.seedAdvisorTurn(t, f.tenantID, "conv_X", "turn_TODAY1", 100, 50, today)
	f.seedAdvisorTurn(t, f.tenantID, "conv_X", "turn_TODAY2", 200, 75, today.Add(30*time.Minute))
	f.seedAdvisorTurn(t, f.tenantID, "conv_X", "turn_OLD", 1000, 500, yesterday)

	a := newLicenseUsageAdapter(f.platform.Storage, f.platform.Clock)
	got, err := a.LLMTokensToday(context.Background(), string(f.tenantID))
	if err != nil {
		t.Fatalf("LLMTokensToday: %v", err)
	}
	want := 100 + 50 + 200 + 75
	if got != want {
		t.Errorf("LLMTokensToday = %d, want %d", got, want)
	}
}

func TestLicenseUsageLLMTokensTodayTenantIsolated(t *testing.T) {
	t.Parallel()
	f := newLicUsageFixture(t)

	now := f.platform.Clock.Now()
	f.seedAdvisorTurn(t, f.tenantID, "conv_A", "turn_A1", 100, 50, now)
	f.seedAdvisorTurn(t, f.otherID, "conv_B", "turn_B1", 999, 1, now)

	a := newLicenseUsageAdapter(f.platform.Storage, f.platform.Clock)
	gotA, _ := a.LLMTokensToday(context.Background(), string(f.tenantID))
	gotB, _ := a.LLMTokensToday(context.Background(), string(f.otherID))
	if gotA != 150 {
		t.Errorf("tenant A = %d, want 150", gotA)
	}
	if gotB != 1000 {
		t.Errorf("tenant B = %d, want 1000", gotB)
	}
}

func TestLicenseUsageLLMTokensTodayEmptyZero(t *testing.T) {
	t.Parallel()
	f := newLicUsageFixture(t)

	a := newLicenseUsageAdapter(f.platform.Storage, f.platform.Clock)
	got, err := a.LLMTokensToday(context.Background(), string(f.tenantID))
	if err != nil {
		t.Fatalf("LLMTokensToday: %v", err)
	}
	if got != 0 {
		t.Errorf("LLMTokensToday = %d, want 0", got)
	}
}

// === todayStartUTC ===

func TestTodayStartUTCNormalizesToMidnight(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 14, 30, 45, 123456789, time.UTC)
	got := todayStartUTC(now)
	want := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("todayStartUTC(%v) = %v, want %v", now, got, want)
	}
}

func TestTodayStartUTCConvertsToUTCFromOtherZone(t *testing.T) {
	t.Parallel()
	// KST(+09)에서 자정 직후 → UTC로는 전날 15시.
	loc := time.FixedZone("KST", 9*3600)
	kst := time.Date(2026, 5, 9, 1, 0, 0, 0, loc) // KST 5/9 01:00 = UTC 5/8 16:00
	got := todayStartUTC(kst)
	want := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("todayStartUTC(KST) = %v, want %v (UTC date base)", got, want)
	}
}
