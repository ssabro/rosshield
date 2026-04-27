package sqliterepo_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/robot/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// fakeSSHTester는 Stage E mock — TestConnection 호출을 기록하고 미리 정한 결과를 반환합니다.
type fakeSSHTester struct {
	calls []sshCall
	err   error
}

type sshCall struct {
	host     string
	port     int
	authType robot.AuthType
	username string
}

func (f *fakeSSHTester) TestConnection(ctx context.Context, host string, port int, authType robot.AuthType, material robot.CredentialMaterial) error {
	f.calls = append(f.calls, sshCall{
		host:     host,
		port:     port,
		authType: authType,
		username: material.Username,
	})
	return f.err
}

// newTestRepoWithTester는 SSHTester가 주입된 Repo를 반환합니다.
func newTestRepoWithTester(t *testing.T, tester robot.SSHTester) (*sqliterepo.Repo, audit.Service, storage.Storage) {
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
		Clock:     clock.System(),
		IDGen:     idgen.NewULID(),
		Audit:     &auditAdapter{svc: auditSvc},
		KEK:       kek,
		SSHTester: tester,
	})
	return repo, auditSvc, store
}

// E5.T5 — TestConnection이 SSHTester mock에 정확한 host/port/authType/material을 전달.
func TestTestConnectionUsesSSHTester(t *testing.T) {
	t.Parallel()
	tester := &fakeSSHTester{}
	repo, _, store := newTestRepoWithTester(t, tester)
	const tenantID = "tn_TC01"
	seedTenant(t, store, tenantID)
	fleetID := createFleetForTest(t, store, repo, tenantID, "Fleet-TC01")

	var robotID string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreateRobot(fleetID)
		req.AuthType = robot.AuthTypePassword
		req.Material = samplePassword()
		res, err := repo.CreateRobot(ctx, tx, req)
		robotID = res.Robot.ID
		return err
	}); err != nil {
		t.Fatalf("CreateRobot: %v", err)
	}

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		return repo.TestConnection(ctx, tx, robotID)
	}); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}

	if len(tester.calls) != 1 {
		t.Fatalf("tester calls = %d, want 1", len(tester.calls))
	}
	c := tester.calls[0]
	if c.host != "10.0.0.10" || c.port != 22 {
		t.Errorf("tester got host=%q port=%d, want 10.0.0.10:22", c.host, c.port)
	}
	if c.authType != robot.AuthTypePassword {
		t.Errorf("tester got authType=%q, want password", c.authType)
	}
	if c.username != "rosshield" {
		t.Errorf("tester got username=%q, want rosshield", c.username)
	}
}

func TestTestConnectionPropagatesSSHError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("dial timeout")
	tester := &fakeSSHTester{err: wantErr}
	repo, _, store := newTestRepoWithTester(t, tester)
	const tenantID = "tn_TC02"
	seedTenant(t, store, tenantID)
	fleetID := createFleetForTest(t, store, repo, tenantID, "Fleet-TC02")

	var robotID string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		res, err := repo.CreateRobot(ctx, tx, sampleCreateRobot(fleetID))
		robotID = res.Robot.ID
		return err
	}); err != nil {
		t.Fatalf("CreateRobot: %v", err)
	}

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		return repo.TestConnection(ctx, tx, robotID)
	})
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestTestConnectionWithoutTesterReturnsConfigError(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepoWithTester(t, nil)
	const tenantID = "tn_TC03"
	seedTenant(t, store, tenantID)
	fleetID := createFleetForTest(t, store, repo, tenantID, "Fleet-TC03")

	var robotID string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		res, err := repo.CreateRobot(ctx, tx, sampleCreateRobot(fleetID))
		robotID = res.Robot.ID
		return err
	}); err != nil {
		t.Fatalf("CreateRobot: %v", err)
	}

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		return repo.TestConnection(ctx, tx, robotID)
	})
	if !errors.Is(err, robot.ErrSSHTesterNotConfigured) {
		t.Errorf("err = %v, want ErrSSHTesterNotConfigured", err)
	}
}

func TestTestConnectionRobotSoftDeletedReturnsNotFound(t *testing.T) {
	t.Parallel()
	tester := &fakeSSHTester{}
	repo, _, store := newTestRepoWithTester(t, tester)
	const tenantID = "tn_TC04"
	seedTenant(t, store, tenantID)
	fleetID := createFleetForTest(t, store, repo, tenantID, "Fleet-TC04")

	var robotID string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		res, err := repo.CreateRobot(ctx, tx, sampleCreateRobot(fleetID))
		robotID = res.Robot.ID
		return err
	}); err != nil {
		t.Fatalf("CreateRobot: %v", err)
	}
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		return repo.DeleteRobot(ctx, tx, robotID)
	}); err != nil {
		t.Fatalf("DeleteRobot: %v", err)
	}

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		return repo.TestConnection(ctx, tx, robotID)
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if len(tester.calls) != 0 {
		t.Errorf("tester should not be called for soft-deleted robot, got %d calls", len(tester.calls))
	}
}

func TestTestConnectionCrossTenantReturnsNotFound(t *testing.T) {
	t.Parallel()
	tester := &fakeSSHTester{}
	repo, _, store := newTestRepoWithTester(t, tester)
	const tA, tB = "tn_TCA1", "tn_TCB1"
	seedTenant(t, store, tA)
	seedTenant(t, store, tB)
	fleetA := createFleetForTest(t, store, repo, tA, "Fleet-A")

	var robotAID string
	if err := store.Tx(tenantCtx(tA), func(ctx context.Context, tx storage.Tx) error {
		res, err := repo.CreateRobot(ctx, tx, sampleCreateRobot(fleetA))
		robotAID = res.Robot.ID
		return err
	}); err != nil {
		t.Fatalf("CreateRobot A: %v", err)
	}

	err := store.Tx(tenantCtx(tB), func(ctx context.Context, tx storage.Tx) error {
		return repo.TestConnection(ctx, tx, robotAID)
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("cross-tenant TestConnection = %v, want ErrNotFound", err)
	}
	if len(tester.calls) != 0 {
		t.Errorf("tester should not be called cross-tenant, got %d calls", len(tester.calls))
	}
}
