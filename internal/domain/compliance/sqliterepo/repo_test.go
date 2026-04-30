package sqliterepo_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/domain/compliance/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// --- fakes ---

// fakeAuditEmitter는 호출 횟수를 카운트하는 stub입니다.
type fakeAuditEmitter struct {
	profileCreated  []compliance.ComplianceProfile
	snapshotCreated []compliance.FrameworkSnapshot
}

func (f *fakeAuditEmitter) EmitProfileCreated(_ context.Context, _ storage.Tx, p compliance.ComplianceProfile) error {
	f.profileCreated = append(f.profileCreated, p)
	return nil
}
func (f *fakeAuditEmitter) EmitSnapshotGenerated(_ context.Context, _ storage.Tx, s compliance.FrameworkSnapshot) error {
	f.snapshotCreated = append(f.snapshotCreated, s)
	return nil
}

// fakeScanReader는 미리 채워진 results를 반환합니다.
type fakeScanReader struct {
	bySession map[string][]compliance.ScanResultView
}

func (f *fakeScanReader) ListResultsForSession(_ context.Context, _ storage.Tx, sessionID string) ([]compliance.ScanResultView, error) {
	return append([]compliance.ScanResultView(nil), f.bySession[sessionID]...), nil
}

// fakeAuditReader는 미리 설정된 head를 반환합니다.
type fakeAuditReader struct {
	head compliance.HeadView
}

func (f *fakeAuditReader) Head(_ context.Context, _ storage.Tx, _ storage.TenantID) (compliance.HeadView, error) {
	return f.head, nil
}

// --- harness ---

func newTestRepo(t *testing.T, scan *fakeScanReader, ar *fakeAuditReader) (*sqliterepo.Repo, *fakeAuditEmitter, storage.Storage) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "compliance.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if scan == nil {
		scan = &fakeScanReader{}
	}
	if ar == nil {
		ar = &fakeAuditReader{}
	}
	emitter := &fakeAuditEmitter{}
	repo := sqliterepo.New(sqliterepo.Deps{
		Clock:       clock.System(),
		IDGen:       idgen.NewULID(),
		Audit:       emitter,
		ScanReader:  scan,
		AuditReader: ar,
	})
	return repo, emitter, store
}

func seedTenant(t *testing.T, store storage.Storage, tenantID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'test', 'desktop_free', ?)`,
			tenantID, now)
		return err
	}); err != nil {
		t.Fatalf("seedTenant: %v", err)
	}
}

func tenantCtx(tenantID string) context.Context {
	return storage.WithTenantID(context.Background(), storage.TenantID(tenantID))
}

// --- tests ---

func TestCreateProfileInsertsAndAuditEmits(t *testing.T) {
	t.Parallel()
	repo, emitter, store := newTestRepo(t, nil, nil)
	const tenantID = "tn_CP1"
	seedTenant(t, store, tenantID)

	var profile compliance.ComplianceProfile
	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		p, err := repo.CreateProfile(ctx, tx, compliance.CreateProfileRequest{
			Framework:        compliance.FrameworkISMSP,
			FrameworkVersion: "2024",
			Enabled:          true,
		})
		profile = p
		return err
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if profile.ID == "" || len(profile.ID) < 4 || profile.ID[:3] != "cp_" {
		t.Errorf("ID = %q, want cp_ prefix", profile.ID)
	}
	if profile.Framework != compliance.FrameworkISMSP {
		t.Errorf("Framework = %s, want isms-p", profile.Framework)
	}
	if !profile.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if string(profile.CustomizationsJSON) != "[]" {
		t.Errorf("CustomizationsJSON = %s, want []", profile.CustomizationsJSON)
	}
	if len(emitter.profileCreated) != 1 {
		t.Errorf("audit emit count = %d, want 1", len(emitter.profileCreated))
	}
}

func TestCreateProfileRejectsDuplicate(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t, nil, nil)
	const tenantID = "tn_CP2"
	seedTenant(t, store, tenantID)

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.CreateProfile(ctx, tx, compliance.CreateProfileRequest{
			Framework:        compliance.FrameworkISO27001,
			FrameworkVersion: "2022",
			Enabled:          true,
		})
		return err
	}); err != nil {
		t.Fatalf("first CreateProfile: %v", err)
	}

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.CreateProfile(ctx, tx, compliance.CreateProfileRequest{
			Framework:        compliance.FrameworkISO27001,
			FrameworkVersion: "2022",
			Enabled:          true,
		})
		return err
	})
	if !errors.Is(err, compliance.ErrProfileExists) {
		t.Errorf("err = %v, want ErrProfileExists", err)
	}
}

func TestCreateProfileRejectsVersionMismatch(t *testing.T) {
	t.Parallel()
	repo, _, store := newTestRepo(t, nil, nil)
	const tenantID = "tn_CP3"
	seedTenant(t, store, tenantID)

	err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.CreateProfile(ctx, tx, compliance.CreateProfileRequest{
			Framework:        compliance.FrameworkISMSP,
			FrameworkVersion: "1999", // 잘못된 버전.
			Enabled:          true,
		})
		return err
	})
	if !errors.Is(err, compliance.ErrFrameworkVersionMismatch) {
		t.Errorf("err = %v, want ErrFrameworkVersionMismatch", err)
	}
}

func TestGenerateSnapshotComputesScoreAndAnchor(t *testing.T) {
	t.Parallel()

	const sessionID = "scan_GS1"
	scan := &fakeScanReader{
		bySession: map[string][]compliance.ScanResultView{
			sessionID: {
				// ISMS-P:2.5.1은 mappedChecks: ["CIS-1.1.1.1"]를 가진 통제 → pass.
				{CheckID: "CIS-1.1.1.1", Outcome: "pass"},
			},
		},
	}
	auditR := &fakeAuditReader{
		head: compliance.HeadView{Seq: 42, Hash: "deadbeef"},
	}
	repo, emitter, store := newTestRepo(t, scan, auditR)
	const tenantID = "tn_GS1"
	seedTenant(t, store, tenantID)

	var profile compliance.ComplianceProfile
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		p, err := repo.CreateProfile(ctx, tx, compliance.CreateProfileRequest{
			Framework:        compliance.FrameworkISMSP,
			FrameworkVersion: "2024",
			Enabled:          true,
		})
		profile = p
		return err
	}); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	var snap compliance.FrameworkSnapshot
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		s, err := repo.GenerateSnapshot(ctx, tx, profile.ID, sessionID)
		snap = s
		return err
	}); err != nil {
		t.Fatalf("GenerateSnapshot: %v", err)
	}

	if snap.ID == "" || len(snap.ID) < 3 || snap.ID[:3] != "fs_" {
		t.Errorf("ID = %q, want fs_ prefix", snap.ID)
	}
	if snap.ChainHeadSeq != 42 || snap.ChainHeadHash != "deadbeef" {
		t.Errorf("anchor = (%d,%s), want (42,deadbeef)", snap.ChainHeadSeq, snap.ChainHeadHash)
	}
	if snap.PassCount < 1 {
		t.Errorf("PassCount = %d, want >= 1 (the mapped control)", snap.PassCount)
	}
	if snap.UnmappedCount == 0 {
		t.Errorf("UnmappedCount = 0, want > 0 (대부분 통제는 mappedChecks 없음)")
	}
	// score는 0~1 범위 + Pass 1개 + Fail 0개 → 1.0.
	if snap.OverallScore < 0.99 {
		t.Errorf("OverallScore = %v, want ~1.0 (only mapped control passed)", snap.OverallScore)
	}
	if len(emitter.snapshotCreated) != 1 {
		t.Errorf("audit snapshot emit count = %d, want 1", len(emitter.snapshotCreated))
	}
}

func TestGenerateSnapshotPersistsStatusesJSON(t *testing.T) {
	t.Parallel()

	const sessionID = "scan_PS1"
	scan := &fakeScanReader{
		bySession: map[string][]compliance.ScanResultView{
			sessionID: {
				{CheckID: "CIS-1.1.1.1", Outcome: "fail"},
			},
		},
	}
	repo, _, store := newTestRepo(t, scan, &fakeAuditReader{head: compliance.HeadView{Seq: 1, Hash: "abc"}})
	const tenantID = "tn_PS1"
	seedTenant(t, store, tenantID)

	var profile compliance.ComplianceProfile
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		p, err := repo.CreateProfile(ctx, tx, compliance.CreateProfileRequest{
			Framework:        compliance.FrameworkISMSP,
			FrameworkVersion: "2024",
			Enabled:          true,
		})
		profile = p
		return err
	}); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GenerateSnapshot(ctx, tx, profile.ID, sessionID)
		return err
	}); err != nil {
		t.Fatalf("GenerateSnapshot: %v", err)
	}

	// ListSnapshots로 재조회 → statuses_json이 deserialize되어 statuses 채워짐.
	var snaps []compliance.FrameworkSnapshot
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		out, err := repo.ListSnapshots(ctx, tx, profile.ID, 0)
		snaps = out
		return err
	}); err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("len(snaps) = %d, want 1", len(snaps))
	}
	if len(snaps[0].Statuses) == 0 {
		t.Errorf("Statuses empty after reload — JSON serde 실패")
	}
	// 적어도 한 통제가 fail이어야 함 (CIS-1.1.1.1 fail → ISMS-P:2.5.1).
	var sawFail bool
	for _, s := range snaps[0].Statuses {
		if s.Status == compliance.StatusFail {
			sawFail = true
			break
		}
	}
	if !sawFail {
		t.Errorf("no Fail status found after reload")
	}
}

func TestGenerateSnapshotEmitsAudit(t *testing.T) {
	t.Parallel()

	scan := &fakeScanReader{}
	repo, emitter, store := newTestRepo(t, scan, &fakeAuditReader{head: compliance.HeadView{Seq: 7, Hash: "hex"}})
	const tenantID = "tn_GA1"
	seedTenant(t, store, tenantID)

	var profile compliance.ComplianceProfile
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		p, err := repo.CreateProfile(ctx, tx, compliance.CreateProfileRequest{
			Framework:        compliance.FrameworkNIST,
			FrameworkVersion: "5.1.1",
			Enabled:          true,
		})
		profile = p
		return err
	}); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GenerateSnapshot(ctx, tx, profile.ID, "scan_no_results")
		return err
	}); err != nil {
		t.Fatalf("GenerateSnapshot: %v", err)
	}

	if len(emitter.profileCreated) != 1 {
		t.Errorf("profile.created emits = %d, want 1", len(emitter.profileCreated))
	}
	if len(emitter.snapshotCreated) != 1 {
		t.Errorf("snapshot.generated emits = %d, want 1", len(emitter.snapshotCreated))
	}
	if emitter.snapshotCreated[0].ChainHeadSeq != 7 {
		t.Errorf("emit ChainHeadSeq = %d, want 7", emitter.snapshotCreated[0].ChainHeadSeq)
	}
}

func TestListSnapshotsReturnsCreatedDESC(t *testing.T) {
	t.Parallel()

	scan := &fakeScanReader{}
	repo, _, store := newTestRepo(t, scan, &fakeAuditReader{head: compliance.HeadView{Seq: 1, Hash: "h"}})
	const tenantID = "tn_LS1"
	seedTenant(t, store, tenantID)

	var profile compliance.ComplianceProfile
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		p, err := repo.CreateProfile(ctx, tx, compliance.CreateProfileRequest{
			Framework:        compliance.FrameworkISO27001,
			FrameworkVersion: "2022",
			Enabled:          true,
		})
		profile = p
		return err
	}); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	// 3개 snapshot 생성 — 생성 순서: A, B, C.
	for i, sid := range []string{"scan_A", "scan_B", "scan_C"} {
		if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.GenerateSnapshot(ctx, tx, profile.ID, sid)
			return err
		}); err != nil {
			t.Fatalf("GenerateSnapshot[%d]: %v", i, err)
		}
		// 간격을 강제 — RFC3339Nano 정밀도 안에서도 created_at 차이를 보장.
		time.Sleep(2 * time.Millisecond)
	}

	var snaps []compliance.FrameworkSnapshot
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		out, err := repo.ListSnapshots(ctx, tx, profile.ID, 0)
		snaps = out
		return err
	}); err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 3 {
		t.Fatalf("len = %d, want 3", len(snaps))
	}
	// DESC 순서: 마지막에 만든 scan_C가 첫 번째.
	if snaps[0].SessionID != "scan_C" {
		t.Errorf("snaps[0].SessionID = %s, want scan_C", snaps[0].SessionID)
	}
	if snaps[2].SessionID != "scan_A" {
		t.Errorf("snaps[2].SessionID = %s, want scan_A", snaps[2].SessionID)
	}
}

func TestListProfilesReturnsAllForTenant(t *testing.T) {
	t.Parallel()

	repo, _, store := newTestRepo(t, nil, nil)
	const tenantID = "tn_LP1"
	seedTenant(t, store, tenantID)

	for _, fv := range []struct {
		f compliance.Framework
		v string
	}{
		{compliance.FrameworkISMSP, "2024"},
		{compliance.FrameworkISO27001, "2022"},
		{compliance.FrameworkNIST, "5.1.1"},
	} {
		if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
			_, err := repo.CreateProfile(ctx, tx, compliance.CreateProfileRequest{
				Framework:        fv.f,
				FrameworkVersion: fv.v,
				Enabled:          true,
			})
			return err
		}); err != nil {
			t.Fatalf("CreateProfile %s: %v", fv.f, err)
		}
	}

	var profiles []compliance.ComplianceProfile
	if err := store.Tx(tenantCtx(tenantID), func(ctx context.Context, tx storage.Tx) error {
		out, err := repo.ListProfiles(ctx, tx)
		profiles = out
		return err
	}); err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(profiles) != 3 {
		t.Fatalf("len = %d, want 3", len(profiles))
	}
}

func TestCrossTenantProfilesIsolated(t *testing.T) {
	t.Parallel()

	repo, _, store := newTestRepo(t, &fakeScanReader{}, &fakeAuditReader{head: compliance.HeadView{Seq: 1, Hash: "h"}})
	const tenantA, tenantB = "tn_XA", "tn_XB"
	seedTenant(t, store, tenantA)
	seedTenant(t, store, tenantB)

	// Tenant A: ISMS-P profile.
	var profileA compliance.ComplianceProfile
	if err := store.Tx(tenantCtx(tenantA), func(ctx context.Context, tx storage.Tx) error {
		p, err := repo.CreateProfile(ctx, tx, compliance.CreateProfileRequest{
			Framework:        compliance.FrameworkISMSP,
			FrameworkVersion: "2024",
			Enabled:          true,
		})
		profileA = p
		return err
	}); err != nil {
		t.Fatalf("CreateProfile A: %v", err)
	}

	// Tenant B: ListProfiles → 빈 리스트.
	var profilesB []compliance.ComplianceProfile
	if err := store.Tx(tenantCtx(tenantB), func(ctx context.Context, tx storage.Tx) error {
		out, err := repo.ListProfiles(ctx, tx)
		profilesB = out
		return err
	}); err != nil {
		t.Fatalf("ListProfiles B: %v", err)
	}
	if len(profilesB) != 0 {
		t.Errorf("Tenant B sees %d profiles from Tenant A — 격리 위반", len(profilesB))
	}

	// Tenant B: GenerateSnapshot(profileA.ID) → ErrProfileNotFound.
	err := store.Tx(tenantCtx(tenantB), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.GenerateSnapshot(ctx, tx, profileA.ID, "scan_X")
		return err
	})
	if !errors.Is(err, compliance.ErrProfileNotFound) {
		t.Errorf("err = %v, want ErrProfileNotFound (cross-tenant 격리)", err)
	}

	// Tenant B: 같은 framework로 CreateProfile은 가능 — UNIQUE는 (tenant, framework) scope.
	if err := store.Tx(tenantCtx(tenantB), func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.CreateProfile(ctx, tx, compliance.CreateProfileRequest{
			Framework:        compliance.FrameworkISMSP,
			FrameworkVersion: "2024",
			Enabled:          true,
		})
		return err
	}); err != nil {
		t.Errorf("Tenant B should be able to create same framework: %v", err)
	}
}
