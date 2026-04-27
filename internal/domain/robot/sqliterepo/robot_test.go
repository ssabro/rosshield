package sqliterepo_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/robot/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

func samplePassword() robot.CredentialMaterial {
	return robot.CredentialMaterial{
		Type:     robot.CredentialTypePassword,
		Username: "rosshield",
		Password: "very-secret-pass-OURMARKER-123456",
	}
}

func samplePrivateKey() robot.CredentialMaterial {
	return robot.CredentialMaterial{
		Type:                 robot.CredentialTypePrivateKey,
		Username:             "rosshield",
		PrivateKeyPEM:        "-----BEGIN OPENSSH PRIVATE KEY-----\nFAKE-PRIVKEY-BLOB\n-----END OPENSSH PRIVATE KEY-----\n",
		PrivateKeyPassphrase: "passphrase-XYZ",
	}
}

// createFleetForTest는 robot 테스트의 전제(fleet 존재) 설정용 헬퍼입니다.
func createFleetForTest(t *testing.T, store storage.Storage, repo *sqliterepo.Repo, tenantID string, name string) string {
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

func sampleCreateRobot(fleetID string) robot.CreateRobotRequest {
	return robot.CreateRobotRequest{
		FleetID:     fleetID,
		Name:        "rosshield-bot-01",
		Host:        "10.0.0.10",
		Port:        22,
		AuthType:    robot.AuthTypePrivateKey,
		Material:    samplePrivateKey(),
		OSDistro:    "ubuntu-24.04",
		ROSDistro:   "jazzy",
		Tags:        []string{"prod", "indoor"},
		Role:        "mobile",
		Criticality: robot.CriticalityHigh,
	}
}

// E5.T2 — FleetID 부재 또는 잘못된 ID 거부.
func TestCreateRobotRequiresFleet(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_RC01"
	seedTenant(t, store, tenantID)

	// 존재하지 않는 FleetID
	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreateRobot("fl_NONEXISTENT")
		_, err := repo.CreateRobot(ctx, tx, req)
		return err
	})
	if !errors.Is(err, robot.ErrFleetNotFound) {
		t.Errorf("err = %v, want ErrFleetNotFound", err)
	}

	// 빈 FleetID
	err = store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreateRobot("")
		_, err := repo.CreateRobot(ctx, tx, req)
		return err
	})
	if !errors.Is(err, robot.ErrRobotEmptyFleet) {
		t.Errorf("err = %v, want ErrRobotEmptyFleet", err)
	}
}

// E5.T3 — DB 파일에 평문 자격증명이 노출되지 않음.
func TestRobotCredentialEncryptedAtRest(t *testing.T) {
	t.Parallel()
	repo, _, store, dbPath := newTestRepoFull(t)
	const tenantID = "tn_RC02"
	seedTenant(t, store, tenantID)
	fleetID := createFleetForTest(t, store, repo, tenantID, "Fleet-RC02")

	mat := samplePassword()
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreateRobot(fleetID)
		req.AuthType = robot.AuthTypePassword
		req.Material = mat
		_, err := repo.CreateRobot(ctx, tx, req)
		return err
	}); err != nil {
		t.Fatalf("CreateRobot: %v", err)
	}

	// store는 이미 열려있음. SQLite WAL이 disk에 sync되도록 close.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, marker := range []string{
		mat.Password,
		"OURMARKER",
	} {
		if bytes.Contains(raw, []byte(marker)) {
			t.Errorf("DB file leaks plaintext marker %q", marker)
		}
	}

	// WAL 파일도 검사 (synchronous=NORMAL이라 -wal에 잔존 가능).
	if walData, err := os.ReadFile(dbPath + "-wal"); err == nil {
		for _, marker := range []string{mat.Password, "OURMARKER"} {
			if bytes.Contains(walData, []byte(marker)) {
				t.Errorf("WAL file leaks plaintext marker %q", marker)
			}
		}
	}
}

// E5.T4 — RotateCredential은 audit emit + 이전 credential revoke.
func TestRobotCredentialRotateIsAudited(t *testing.T) {
	t.Parallel()
	repo, auditSvc, store := newTestRepo(t)
	const tenantID = "tn_RC03"
	seedTenant(t, store, tenantID)
	fleetID := createFleetForTest(t, store, repo, tenantID, "Fleet-RC03")

	var robotID, oldCredID string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		res, err := repo.CreateRobot(ctx, tx, sampleCreateRobot(fleetID))
		if err != nil {
			return err
		}
		robotID = res.Robot.ID
		oldCredID = res.Robot.CredentialID
		return nil
	}); err != nil {
		t.Fatalf("CreateRobot: %v", err)
	}

	// 부팅 후 audit head: fleet.created(1) + robot.created(2) = 2
	headBeforeRotate := getAuditSeq(t, store, auditSvc, tenantID)
	if headBeforeRotate != 2 {
		t.Errorf("seq before rotate = %d, want 2 (fleet+robot)", headBeforeRotate)
	}

	// Rotate
	newMat := samplePrivateKey()
	newMat.PrivateKeyPassphrase = "new-passphrase"
	var rotateRes robot.RotateCredentialResult
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.RotateCredential(ctx, tx, robot.RotateCredentialRequest{
			RobotID:  robotID,
			Material: newMat,
		})
		rotateRes = r
		return err
	}); err != nil {
		t.Fatalf("RotateCredential: %v", err)
	}

	if rotateRes.OldCredentialID != oldCredID {
		t.Errorf("OldCredentialID = %q, want %q", rotateRes.OldCredentialID, oldCredID)
	}
	if rotateRes.NewCredentialID == oldCredID {
		t.Error("NewCredentialID equals OldCredentialID")
	}
	if !strings.HasPrefix(rotateRes.NewCredentialID, "cr_") {
		t.Errorf("NewCredentialID = %q, want cr_ prefix", rotateRes.NewCredentialID)
	}

	// audit head: fleet.created(1) + robot.created(2) + credential.rotated(3)
	headAfter := getAuditSeq(t, store, auditSvc, tenantID)
	if headAfter != 3 {
		t.Errorf("seq after rotate = %d, want 3", headAfter)
	}

	// 새 material로 GetCredentialMaterial 결과 일치
	var got robot.CredentialMaterial
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		m, err := repo.GetCredentialMaterial(ctx, tx, robotID)
		got = m
		return err
	}); err != nil {
		t.Fatalf("GetCredentialMaterial: %v", err)
	}
	if got != newMat {
		t.Errorf("rotated material mismatch: got %+v, want %+v", got, newMat)
	}
}

// E5.T7 — Robot soft delete는 audit row를 보존(audit append-only) + GetRobot은 ErrNotFound.
func TestRobotDeleteKeepsAuditReferences(t *testing.T) {
	t.Parallel()
	repo, auditSvc, store := newTestRepo(t)
	const tenantID = "tn_RC04"
	seedTenant(t, store, tenantID)
	fleetID := createFleetForTest(t, store, repo, tenantID, "Fleet-RC04")

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

	// GetRobot은 ErrNotFound (soft-deleted).
	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GetRobot(ctx, tx, robotID)
		return err
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("GetRobot after delete = %v, want ErrNotFound", err)
	}

	// audit head: fleet.created(1) + robot.created(2) + robot.deleted(3) = 3
	head := getAuditSeq(t, store, auditSvc, tenantID)
	if head != 3 {
		t.Errorf("seq after delete = %d, want 3", head)
	}

	// audit row 자체는 immutable 보존 — Verify로 chain 검증.
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		res, err := auditSvc.Verify(ctx, tx, storage.TenantID(tenantID), 1, 3)
		if err != nil {
			return err
		}
		if !res.OK {
			t.Errorf("audit chain Verify after delete: %+v", res)
		}
		return nil
	}); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	// 두 번째 DeleteRobot은 ErrNotFound (Phase 1 — 멱등 아님, 명시적 한 번만).
	err = store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		return repo.DeleteRobot(ctx, tx, robotID)
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("second DeleteRobot = %v, want ErrNotFound", err)
	}
}

// 보조 — CreateRobot acceptance 케이스.
func TestCreateRobotAppliesDefaultsAndPersists(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_RC05"
	seedTenant(t, store, tenantID)
	fleetID := createFleetForTest(t, store, repo, tenantID, "Fleet-RC05")

	var res robot.CreateRobotResult
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreateRobot(fleetID)
		req.Port = 0         // → 22 default
		req.Criticality = "" // → medium default
		req.AuthType = ""    // → privateKey default
		req.Material = samplePrivateKey()
		req.Tags = nil // → []
		r, err := repo.CreateRobot(ctx, tx, req)
		res = r
		return err
	}); err != nil {
		t.Fatalf("CreateRobot: %v", err)
	}

	if !strings.HasPrefix(res.Robot.ID, "ro_") {
		t.Errorf("Robot ID = %q, want ro_ prefix", res.Robot.ID)
	}
	if !strings.HasPrefix(res.Robot.CredentialID, "cr_") {
		t.Errorf("CredentialID = %q, want cr_ prefix", res.Robot.CredentialID)
	}
	if res.Robot.Port != 22 {
		t.Errorf("Port = %d, want 22", res.Robot.Port)
	}
	if res.Robot.Criticality != robot.CriticalityMedium {
		t.Errorf("Criticality = %q, want medium", res.Robot.Criticality)
	}
	if res.Robot.AuthType != robot.AuthTypePrivateKey {
		t.Errorf("AuthType = %q, want privateKey", res.Robot.AuthType)
	}
	if res.Robot.Tags == nil || len(res.Robot.Tags) != 0 {
		t.Errorf("Tags = %v, want empty slice", res.Robot.Tags)
	}
	if res.Credential.Type != robot.CredentialTypePrivateKey {
		t.Errorf("Credential.Type = %q, want privateKey", res.Credential.Type)
	}
	if len(res.Credential.EncryptedPayload) == 0 {
		t.Error("Credential.EncryptedPayload empty")
	}
}

// 보조 — name uniqueness는 fleet 단위.
func TestCreateRobotRejectsDuplicateNameInSameFleet(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_RC06"
	seedTenant(t, store, tenantID)
	fleetID := createFleetForTest(t, store, repo, tenantID, "Fleet-RC06")

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.CreateRobot(ctx, tx, sampleCreateRobot(fleetID))
		return err
	}); err != nil {
		t.Fatalf("first CreateRobot: %v", err)
	}

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreateRobot(fleetID)
		req.Host = "10.0.0.20" // host port는 다르게 → name 충돌만
		_, err := repo.CreateRobot(ctx, tx, req)
		return err
	})
	if !errors.Is(err, robot.ErrRobotNameDuplicate) {
		t.Errorf("err = %v, want ErrRobotNameDuplicate", err)
	}
}

func TestCreateRobotAllowsSameNameAcrossFleets(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_RC07"
	seedTenant(t, store, tenantID)
	fleetA := createFleetForTest(t, store, repo, tenantID, "Fleet-A")
	fleetB := createFleetForTest(t, store, repo, tenantID, "Fleet-B")

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.CreateRobot(ctx, tx, sampleCreateRobot(fleetA))
		return err
	}); err != nil {
		t.Fatalf("CreateRobot A: %v", err)
	}

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreateRobot(fleetB)
		req.Host = "10.0.0.20" // host port는 다르게
		_, err := repo.CreateRobot(ctx, tx, req)
		return err
	}); err != nil {
		t.Errorf("same name across fleets should succeed: %v", err)
	}
}

func TestCreateRobotRejectsDuplicateHostPort(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_RC08"
	seedTenant(t, store, tenantID)
	fleetID := createFleetForTest(t, store, repo, tenantID, "Fleet-RC08")

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.CreateRobot(ctx, tx, sampleCreateRobot(fleetID))
		return err
	}); err != nil {
		t.Fatalf("first CreateRobot: %v", err)
	}

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreateRobot(fleetID)
		req.Name = "rosshield-bot-different" // 이름 다르게 → host:port 충돌만
		_, err := repo.CreateRobot(ctx, tx, req)
		return err
	})
	if !errors.Is(err, robot.ErrRobotHostPortConflict) {
		t.Errorf("err = %v, want ErrRobotHostPortConflict", err)
	}
}

func TestCreateRobotRejectsAuthTypeMaterialMismatch(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_RC09"
	seedTenant(t, store, tenantID)
	fleetID := createFleetForTest(t, store, repo, tenantID, "Fleet-RC09")

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreateRobot(fleetID)
		req.AuthType = robot.AuthTypePassword
		req.Material = samplePrivateKey() // 불일치
		_, err := repo.CreateRobot(ctx, tx, req)
		return err
	})
	if !errors.Is(err, robot.ErrRobotInvalidAuthType) {
		t.Errorf("err = %v, want ErrRobotInvalidAuthType", err)
	}
}

func TestGetCredentialMaterialRoundtrip(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_RC10"
	seedTenant(t, store, tenantID)
	fleetID := createFleetForTest(t, store, repo, tenantID, "Fleet-RC10")

	want := samplePassword()
	var robotID string
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		req := sampleCreateRobot(fleetID)
		req.AuthType = robot.AuthTypePassword
		req.Material = want
		res, err := repo.CreateRobot(ctx, tx, req)
		robotID = res.Robot.ID
		return err
	}); err != nil {
		t.Fatalf("CreateRobot: %v", err)
	}

	var got robot.CredentialMaterial
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		m, err := repo.GetCredentialMaterial(ctx, tx, robotID)
		got = m
		return err
	}); err != nil {
		t.Fatalf("GetCredentialMaterial: %v", err)
	}
	if got != want {
		t.Errorf("material mismatch: got %+v, want %+v", got, want)
	}
}

func TestListRobotsByFleetAndAll(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tenantID = "tn_RC11"
	seedTenant(t, store, tenantID)
	fleetA := createFleetForTest(t, store, repo, tenantID, "Fleet-A11")
	fleetB := createFleetForTest(t, store, repo, tenantID, "Fleet-B11")

	mk := func(fleetID, name, host string) {
		t.Helper()
		if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
			req := sampleCreateRobot(fleetID)
			req.Name = name
			req.Host = host
			_, err := repo.CreateRobot(ctx, tx, req)
			return err
		}); err != nil {
			t.Fatalf("CreateRobot %s: %v", name, err)
		}
	}
	mk(fleetA, "a1", "10.0.0.1")
	mk(fleetA, "a2", "10.0.0.2")
	mk(fleetB, "b1", "10.0.0.3")

	checkList := func(filter string, wantN int) {
		t.Helper()
		var list []robot.Robot
		if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
			l, err := repo.ListRobots(ctx, tx, filter)
			list = l
			return err
		}); err != nil {
			t.Fatalf("ListRobots %q: %v", filter, err)
		}
		if len(list) != wantN {
			t.Errorf("ListRobots(%q) len = %d, want %d", filter, len(list), wantN)
		}
	}
	checkList("", 3) // 전체
	checkList(fleetA, 2)
	checkList(fleetB, 1)
}

func TestRobotCrossTenantBlocked(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t)
	const tA, tB = "tn_RCA1", "tn_RCB1"
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

	// tenant B가 A의 robot ID로 GetRobot → ErrNotFound
	err := store.Tx(tenantCtx(tB), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GetRobot(ctx, tx, robotAID)
		return err
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("cross-tenant GetRobot = %v, want ErrNotFound", err)
	}

	// ListRobots in B → 0
	err = store.Tx(tenantCtx(tB), func(ctx context.Context, tx storage.Tx) error {
		list, err := repo.ListRobots(ctx, tx, "")
		if err != nil {
			return err
		}
		if len(list) != 0 {
			t.Errorf("tenant B sees %d robots, want 0", len(list))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ListRobots B: %v", err)
	}

	// GetCredentialMaterial cross-tenant → ErrNotFound
	err = store.Tx(tenantCtx(tB), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GetCredentialMaterial(ctx, tx, robotAID)
		return err
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("cross-tenant GetCredentialMaterial = %v, want ErrNotFound", err)
	}
}

// --- helpers ---

func getAuditSeq(t *testing.T, store storage.Storage, auditSvc audit.Service, tenantID string) int {
	t.Helper()
	var head audit.ChainHead
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		h, err := auditSvc.Head(ctx, tx, storage.TenantID(tenantID))
		head = h
		return err
	}); err != nil {
		t.Fatalf("Audit.Head: %v", err)
	}
	return int(head.Seq)
}
