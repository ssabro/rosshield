package sqliterepo_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/robot/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// auditAdapter는 audit.Service를 robot.AuditEmitter로 감싸는 테스트용 구현입니다.
// (cmd/rosshield-server/bootstrap.go에 동일 패턴이 결선됨 — Stage A·C에서)
type auditAdapter struct {
	svc audit.Service
}

func (a *auditAdapter) EmitFleetCreated(ctx context.Context, tx storage.Tx, f robot.Fleet) error {
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: f.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "fleet.created",
		Target:   audit.Target{Type: "fleet", ID: f.ID},
		Payload:  []byte(`{"name":"` + f.Name + `"}`),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

func (a *auditAdapter) EmitRobotCreated(ctx context.Context, tx storage.Tx, r robot.Robot, credentialID string) error {
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: r.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "robot.created",
		Target:   audit.Target{Type: "robot", ID: r.ID},
		Payload:  []byte(`{"name":"` + r.Name + `","credentialId":"` + credentialID + `"}`),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

func (a *auditAdapter) EmitRobotDeleted(ctx context.Context, tx storage.Tx, robotID string, tenantID storage.TenantID) error {
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: tenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "robot.deleted",
		Target:   audit.Target{Type: "robot", ID: robotID},
		Payload:  []byte(`{}`),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

func (a *auditAdapter) EmitCredentialRotated(ctx context.Context, tx storage.Tx, robotID, oldCredID, newCredID string, tenantID storage.TenantID) error {
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: tenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "credential.rotated",
		Target:   audit.Target{Type: "robot", ID: robotID},
		Payload:  []byte(`{"oldCredentialId":"` + oldCredID + `","newCredentialId":"` + newCredID + `"}`),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

func newTestRepo(t *testing.T) (*sqliterepo.Repo, audit.Service, storage.Storage) {
	t.Helper()
	repo, auditSvc, store, _ := newTestRepoFull(t)
	return repo, auditSvc, store
}

// newTestRepoFull은 테스트가 DB 파일 경로를 필요로 하는 경우(T3 — DB grep 검증) 사용합니다.
func newTestRepoFull(t *testing.T) (*sqliterepo.Repo, audit.Service, storage.Storage, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "robot.db")
	kekPath := filepath.Join(dir, "credential.kek")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	kek, err := robot.LoadOrCreateKEK(kekPath)
	if err != nil {
		t.Fatalf("LoadOrCreateKEK: %v", err)
	}

	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clock.System()})
	repo := sqliterepo.New(sqliterepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
		Audit: &auditAdapter{svc: auditSvc},
		KEK:   kek,
	})
	return repo, auditSvc, store, dbPath
}

// seedTenant는 fleets.tenant_id FK를 만족시키기 위해 직접 tenants row를 INSERT합니다.
// (E3 tenant 도메인 의존을 피하기 위한 raw INSERT — 테스트 단순화.)
func seedTenant(t *testing.T, store storage.Storage, tenantID string) {
	t.Helper()
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO tenants (id, name, plan, created_at)
VALUES (?, ?, 'desktop_free', ?)`,
			tenantID, "test-"+tenantID, time.Now().UTC().Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatalf("seedTenant %s: %v", tenantID, err)
	}
}

func tenantCtx(tenantID string) context.Context {
	return storage.WithTenantID(context.Background(), storage.TenantID(tenantID))
}

func sampleCreate() robot.CreateFleetRequest {
	return robot.CreateFleetRequest{
		Name:        "Mobile Robots A",
		Description: "test fleet",
		Policy: robot.FleetPolicy{
			DefaultBaselineID:  "cis-ubuntu-24.04",
			DefaultLevel:       robot.LevelL1,
			DefaultCriticality: robot.CriticalityMedium,
			ScanSchedule:       "@every 1h",
		},
	}
}

// E5.T1 — 핵심 acceptance.
func TestCreateFleetWithDefaultPolicy(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_TEST01"
	seedTenant(t, store, tenantID)

	var fleet robot.Fleet
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		f, err := repo.CreateFleet(ctx, tx, sampleCreate())
		fleet = f
		return err
	}); err != nil {
		t.Fatalf("CreateFleet: %v", err)
	}

	if !strings.HasPrefix(fleet.ID, "fl_") {
		t.Errorf("fleet ID = %q, want fl_ prefix", fleet.ID)
	}
	if fleet.TenantID != storage.TenantID(tenantID) {
		t.Errorf("TenantID = %q, want %q", fleet.TenantID, tenantID)
	}
	if fleet.Name != "Mobile Robots A" {
		t.Errorf("Name = %q, want %q", fleet.Name, "Mobile Robots A")
	}
	if fleet.Policy.DefaultBaselineID != "cis-ubuntu-24.04" {
		t.Errorf("DefaultBaselineID = %q, want cis-ubuntu-24.04", fleet.Policy.DefaultBaselineID)
	}
	if fleet.Policy.DefaultLevel != robot.LevelL1 {
		t.Errorf("DefaultLevel = %q, want L1", fleet.Policy.DefaultLevel)
	}
	if fleet.Policy.DefaultCriticality != robot.CriticalityMedium {
		t.Errorf("DefaultCriticality = %q, want medium", fleet.Policy.DefaultCriticality)
	}
	if fleet.Policy.ScanSchedule != "@every 1h" {
		t.Errorf("ScanSchedule = %q, want @every 1h", fleet.Policy.ScanSchedule)
	}
	if fleet.DeletedAt != nil {
		t.Errorf("DeletedAt = %v, want nil", fleet.DeletedAt)
	}
	if fleet.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if !fleet.UpdatedAt.Equal(fleet.CreatedAt) {
		t.Errorf("UpdatedAt = %v, want = CreatedAt %v on insert", fleet.UpdatedAt, fleet.CreatedAt)
	}
}

func TestCreateFleetEmitsAudit(t *testing.T) {
	t.Parallel()
	repo, auditSvc, store := newTestRepo(t)
	const tenantID = "tn_TEST02"
	seedTenant(t, store, tenantID)

	var fleet robot.Fleet
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		f, err := repo.CreateFleet(ctx, tx, sampleCreate())
		fleet = f
		return err
	}); err != nil {
		t.Fatalf("CreateFleet: %v", err)
	}

	var head audit.ChainHead
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		h, err := auditSvc.Head(ctx, tx, fleet.TenantID)
		head = h
		return err
	}); err != nil {
		t.Fatalf("Audit.Head: %v", err)
	}
	if head.Seq != 1 {
		t.Errorf("audit head seq = %d, want 1 (fleet.created)", head.Seq)
	}
}

func TestCreateFleetRejectsEmptyName(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_TEST03"
	seedTenant(t, store, tenantID)

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreate()
		req.Name = "   "
		_, err := repo.CreateFleet(ctx, tx, req)
		return err
	})
	if !errors.Is(err, robot.ErrFleetEmptyName) {
		t.Errorf("err = %v, want ErrFleetEmptyName", err)
	}
}

func TestCreateFleetRejectsTooLongName(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_TEST04"
	seedTenant(t, store, tenantID)

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreate()
		req.Name = strings.Repeat("x", 201)
		_, err := repo.CreateFleet(ctx, tx, req)
		return err
	})
	if !errors.Is(err, robot.ErrFleetNameTooLong) {
		t.Errorf("err = %v, want ErrFleetNameTooLong", err)
	}
}

func TestCreateFleetRejectsInvalidLevel(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_TEST05"
	seedTenant(t, store, tenantID)

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreate()
		req.Policy.DefaultLevel = "L3"
		_, err := repo.CreateFleet(ctx, tx, req)
		return err
	})
	if !errors.Is(err, robot.ErrFleetInvalidLevel) {
		t.Errorf("err = %v, want ErrFleetInvalidLevel", err)
	}
}

func TestCreateFleetRejectsInvalidCriticality(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_TEST06"
	seedTenant(t, store, tenantID)

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreate()
		req.Policy.DefaultCriticality = "ultra"
		_, err := repo.CreateFleet(ctx, tx, req)
		return err
	})
	if !errors.Is(err, robot.ErrFleetInvalidCritical) {
		t.Errorf("err = %v, want ErrFleetInvalidCritical", err)
	}
}

func TestCreateFleetRejectsDuplicateName(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_TEST07"
	seedTenant(t, store, tenantID)

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.CreateFleet(ctx, tx, sampleCreate())
		return err
	}); err != nil {
		t.Fatalf("first CreateFleet: %v", err)
	}

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.CreateFleet(ctx, tx, sampleCreate())
		return err
	})
	if !errors.Is(err, robot.ErrFleetNameDuplicate) {
		t.Errorf("err = %v, want ErrFleetNameDuplicate", err)
	}
}

// R3-5 — partial unique index 동작: soft-deleted fleet 후 같은 이름 재등록 허용.
func TestCreateFleetSoftDeletedNameReusable(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_TEST08"
	seedTenant(t, store, tenantID)

	var firstID string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		f, err := repo.CreateFleet(ctx, tx, sampleCreate())
		firstID = f.ID
		return err
	}); err != nil {
		t.Fatalf("first CreateFleet: %v", err)
	}

	// raw UPDATE로 soft delete 표시 (DeleteFleet 메서드는 Stage C에서).
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fleets SET deleted_at = ? WHERE id = ?`,
			time.Now().UTC().Format(time.RFC3339Nano), firstID)
		return err
	}); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// 같은 이름으로 재등록 허용 — partial unique index가 deleted row를 인덱싱하지 않음.
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.CreateFleet(ctx, tx, sampleCreate())
		return err
	}); err != nil {
		t.Errorf("re-create after soft delete: %v, want nil", err)
	}
}

func TestCreateFleetRequiresTenantContext(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)

	// Bootstrap Tx는 TenantID가 빈 값 — CreateFleet은 ErrTenantMissing.
	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.CreateFleet(ctx, tx, sampleCreate())
		return err
	})
	if !errors.Is(err, storage.ErrTenantMissing) {
		t.Errorf("err = %v, want ErrTenantMissing", err)
	}
}

func TestGetFleetReturnsCreated(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_TEST09"
	seedTenant(t, store, tenantID)

	var created robot.Fleet
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		f, err := repo.CreateFleet(ctx, tx, sampleCreate())
		created = f
		return err
	}); err != nil {
		t.Fatalf("CreateFleet: %v", err)
	}

	var fetched robot.Fleet
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		f, err := repo.GetFleet(ctx, tx, created.ID)
		fetched = f
		return err
	}); err != nil {
		t.Fatalf("GetFleet: %v", err)
	}
	if fetched.ID != created.ID {
		t.Errorf("ID = %q, want %q", fetched.ID, created.ID)
	}
	if fetched.Name != created.Name {
		t.Errorf("Name = %q, want %q", fetched.Name, created.Name)
	}
	if fetched.Policy != created.Policy {
		t.Errorf("Policy = %+v, want %+v", fetched.Policy, created.Policy)
	}
}

func TestGetFleetIgnoresSoftDeleted(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_TEST10"
	seedTenant(t, store, tenantID)

	var id string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		f, err := repo.CreateFleet(ctx, tx, sampleCreate())
		id = f.ID
		return err
	}); err != nil {
		t.Fatalf("CreateFleet: %v", err)
	}

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fleets SET deleted_at = ? WHERE id = ?`,
			time.Now().UTC().Format(time.RFC3339Nano), id)
		return err
	}); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GetFleet(ctx, tx, id)
		return err
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound for soft-deleted fleet", err)
	}
}

func TestGetFleetCrossTenantBlocked(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantA, tenantB = "tn_TESTA1", "tn_TESTB1"
	seedTenant(t, store, tenantA)
	seedTenant(t, store, tenantB)

	var idA string
	if err := store.Tx(tenantCtx(tenantA), func(ctx context.Context, tx storage.Tx) error {
		f, err := repo.CreateFleet(ctx, tx, sampleCreate())
		idA = f.ID
		return err
	}); err != nil {
		t.Fatalf("CreateFleet A: %v", err)
	}

	// tenant B가 A의 fleet ID로 조회 → ErrNotFound.
	err := store.Tx(tenantCtx(tenantB), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GetFleet(ctx, tx, idA)
		return err
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("cross-tenant read err = %v, want ErrNotFound", err)
	}
}

func TestListFleetsReturnsActiveOnly(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_TEST11"
	seedTenant(t, store, tenantID)

	// 활성 2개, 삭제 1개.
	create := func(name string) string {
		t.Helper()
		var id string
		if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
			req := sampleCreate()
			req.Name = name
			f, err := repo.CreateFleet(ctx, tx, req)
			id = f.ID
			return err
		}); err != nil {
			t.Fatalf("CreateFleet %s: %v", name, err)
		}
		return id
	}
	create("alpha")
	create("beta")
	deletedID := create("gamma")

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fleets SET deleted_at = ? WHERE id = ?`,
			time.Now().UTC().Format(time.RFC3339Nano), deletedID)
		return err
	}); err != nil {
		t.Fatalf("soft delete gamma: %v", err)
	}

	var list []robot.Fleet
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		l, err := repo.ListFleets(ctx, tx)
		list = l
		return err
	}); err != nil {
		t.Fatalf("ListFleets: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("len(list) = %d, want 2 (gamma soft-deleted)", len(list))
	}
	for _, f := range list {
		if f.Name == "gamma" {
			t.Errorf("ListFleets returned soft-deleted gamma: %+v", f)
		}
	}
}

func TestListFleetsCrossTenantBlocked(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantA, tenantB = "tn_TESTA2", "tn_TESTB2"
	seedTenant(t, store, tenantA)
	seedTenant(t, store, tenantB)

	if err := store.Tx(tenantCtx(tenantA), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.CreateFleet(ctx, tx, sampleCreate())
		return err
	}); err != nil {
		t.Fatalf("CreateFleet A: %v", err)
	}

	var listB []robot.Fleet
	if err := store.Tx(tenantCtx(tenantB), func(ctx context.Context, tx storage.Tx) error {
		l, err := repo.ListFleets(ctx, tx)
		listB = l
		return err
	}); err != nil {
		t.Fatalf("ListFleets B: %v", err)
	}
	if len(listB) != 0 {
		t.Errorf("tenant B sees %d fleets, want 0", len(listB))
	}
}

// MarshalPolicy/UnmarshalPolicy 라운드트립.
func TestPolicyRoundtrip(t *testing.T) {
	t.Parallel()
	cases := []robot.FleetPolicy{
		{},
		{DefaultLevel: robot.LevelL2},
		{DefaultBaselineID: "cis-ubuntu-24.04", DefaultLevel: robot.LevelL1, DefaultCriticality: robot.CriticalityHigh, ScanSchedule: "@every 6h"},
	}
	for i, want := range cases {
		raw, err := robot.MarshalPolicy(want)
		if err != nil {
			t.Fatalf("[%d] Marshal: %v", i, err)
		}
		got, err := robot.UnmarshalPolicy(raw)
		if err != nil {
			t.Fatalf("[%d] Unmarshal: %v", i, err)
		}
		if got != want {
			t.Errorf("[%d] roundtrip = %+v, want %+v", i, got, want)
		}
	}
}
