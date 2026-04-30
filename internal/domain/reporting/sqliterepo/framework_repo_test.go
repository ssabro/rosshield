package sqliterepo_test

// framework_repo_test.go — E18 Phase 2 framework 리포트 테스트.
//
// 별도 harness(newFrameworkHarness)는 newHarness 위에 fakeFrameworkBuilder/fakeCompliance
// 만 추가 + compliance_profiles/framework_snapshots 시드.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/domain/reporting/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"

	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
)

// === fakes ===

type fakeFrameworkBuilder struct {
	output []byte
	called int
	last   reporting.FrameworkPDFInput
	err    error
}

func (f *fakeFrameworkBuilder) BuildFramework(input reporting.FrameworkPDFInput) ([]byte, error) {
	f.called++
	f.last = input
	if f.err != nil {
		return nil, f.err
	}
	return f.output, nil
}

type fakeCompliance struct {
	view reporting.FrameworkComplianceView
	err  error
}

func (c *fakeCompliance) LoadProfileSnapshot(_ context.Context, _ storage.Tx, profileID, snapshotID string) (reporting.FrameworkComplianceView, error) {
	if c.err != nil {
		return reporting.FrameworkComplianceView{}, c.err
	}
	v := c.view
	v.Profile.ID = profileID
	v.Snapshot.ID = snapshotID
	return v, nil
}

// === harness ===

type frameworkHarness struct {
	repo     *sqliterepo.Repo
	store    storage.Storage
	tenantID storage.TenantID
	builder  *fakeFrameworkBuilder
	comp     *fakeCompliance
}

const fwTestTenant = "tn_FWREP"

func newFrameworkHarness(t *testing.T) *frameworkHarness {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: filepath.Join(dir, "framework.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clock.System()})
	builder := &fakeFrameworkBuilder{output: []byte("FRAMEWORK-PDF-V1")}
	comp := &fakeCompliance{
		view: reporting.FrameworkComplianceView{
			Profile: reporting.FrameworkProfileView{
				Framework:        "isms-p",
				FrameworkVersion: "2024",
			},
			Snapshot: reporting.FrameworkSnapshotView{
				OverallScore:       0.83,
				PassCount:          25,
				FailCount:          3,
				PartialCount:       2,
				NotApplicableCount: 5,
				UnmappedCount:      10,
				ChainHeadSeq:       42,
				ChainHeadHash:      "deadbeef",
				CreatedAt:          time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
				Statuses: []reporting.FrameworkControlStatusView{
					{ControlID: "ISMS-P:2.5.1", Title: "접근 권한", Status: "pass", PassCount: 1},
					{ControlID: "ISMS-P:2.5.2", Title: "패스워드", Status: "fail", FailCount: 2},
				},
			},
		},
	}

	repo := sqliterepo.New(sqliterepo.Deps{
		Clock:            clock.System(),
		IDGen:            idgen.NewULID(),
		Audit:            &auditAdapter{svc: auditSvc},
		Builder:          &fakeBuilder{output: []byte("session-pdf")}, // 미사용 (Generate 호출 X)
		FrameworkBuilder: builder,
		Compliance:       comp,
	})

	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'fw-test', 'desktop_free', ?)`,
			fwTestTenant, now); err != nil {
			return err
		}
		// compliance_profile + framework_snapshot 시드 (Generate가 LoadProfileSnapshot으로 회수하므로 직접 사용 X,
		// 하지만 framework_reports.profile_id·snapshot_id FK 충족용으로 필요).
		if _, err := tx.Exec(ctx, `INSERT INTO compliance_profiles (id, tenant_id, framework, framework_version, enabled, customizations_json, created_at, updated_at) VALUES (?, ?, 'isms-p', '2024', 1, '[]', ?, ?)`,
			"cp_FWA", fwTestTenant, now, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO framework_snapshots (id, tenant_id, profile_id, overall_score, pass_count, fail_count, partial_count, not_applicable_count, unmapped_count, chain_head_seq, chain_head_hash, statuses_json, created_at) VALUES (?, ?, ?, 0.83, 25, 3, 2, 5, 10, 42, 'deadbeef', '[]', ?)`,
			"fs_FWA", fwTestTenant, "cp_FWA", now); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	return &frameworkHarness{
		repo:     repo,
		store:    store,
		tenantID: fwTestTenant,
		builder:  builder,
		comp:     comp,
	}
}

func fwCtx(tenantID storage.TenantID) context.Context {
	return storage.WithTenantID(context.Background(), tenantID)
}

// === tests ===

func TestGenerateFrameworkPersistsAndCallsBuilder(t *testing.T) {
	t.Parallel()
	h := newFrameworkHarness(t)

	var report reporting.FrameworkReport
	if err := h.store.Tx(fwCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		r, err := h.repo.GenerateFramework(ctx, tx, reporting.GenerateFrameworkRequest{
			ProfileID:   "cp_FWA",
			SnapshotID:  "fs_FWA",
			GeneratedBy: "user_X",
		})
		report = r
		return err
	}); err != nil {
		t.Fatalf("GenerateFramework: %v", err)
	}
	if !strings.HasPrefix(report.ID, "frep_") {
		t.Errorf("ID = %q, want frep_ prefix", report.ID)
	}
	if h.builder.called != 1 {
		t.Errorf("builder called %d times, want 1", h.builder.called)
	}
	if h.builder.last.ProfileID != "cp_FWA" {
		t.Errorf("input.ProfileID = %q, want cp_FWA", h.builder.last.ProfileID)
	}
	if h.builder.last.Stats.TotalControls != 2 {
		t.Errorf("Stats.TotalControls = %d, want 2", h.builder.last.Stats.TotalControls)
	}
	if !report.Signature.IsZero() {
		t.Errorf("Signature should be zero (Generate doesn't sign)")
	}
}

func TestGenerateFrameworkValidatesInput(t *testing.T) {
	t.Parallel()
	h := newFrameworkHarness(t)

	cases := []struct {
		name    string
		req     reporting.GenerateFrameworkRequest
		wantErr error
	}{
		{"missing profile", reporting.GenerateFrameworkRequest{SnapshotID: "fs_FWA"}, reporting.ErrFrameworkProfileMissing},
		{"missing snapshot", reporting.GenerateFrameworkRequest{ProfileID: "cp_FWA"}, reporting.ErrFrameworkSnapshotMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.store.Tx(fwCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
				_, e := h.repo.GenerateFramework(ctx, tx, tc.req)
				return e
			})
			if err != tc.wantErr {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSignFrameworkUpdatesSignature(t *testing.T) {
	t.Parallel()
	h := newFrameworkHarness(t)

	var generated reporting.FrameworkReport
	if err := h.store.Tx(fwCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		r, err := h.repo.GenerateFramework(ctx, tx, reporting.GenerateFrameworkRequest{
			ProfileID:  "cp_FWA",
			SnapshotID: "fs_FWA",
		})
		generated = r
		return err
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	sig := make([]byte, reporting.Ed25519SignatureSize)
	for i := range sig {
		sig[i] = byte(0x80 | (i & 0x7f))
	}
	signedAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

	var signed reporting.FrameworkReport
	if err := h.store.Tx(fwCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		r, err := h.repo.SignFramework(ctx, tx, generated.ID, "key_TEST", sig, 99, "abcdef", signedAt)
		signed = r
		return err
	}); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if signed.Signature.IsZero() {
		t.Errorf("after Sign, IsZero should be false")
	}
	if signed.Signature.SignerKeyID != "key_TEST" {
		t.Errorf("SignerKeyID = %q, want key_TEST", signed.Signature.SignerKeyID)
	}
	if signed.Signature.ChainHeadSeq != 99 {
		t.Errorf("ChainHeadSeq = %d, want 99", signed.Signature.ChainHeadSeq)
	}

	// 두 번째 Sign은 ErrAlreadySigned.
	err := h.store.Tx(fwCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.SignFramework(ctx, tx, generated.ID, "key_X", sig, 100, "xx", signedAt)
		return e
	})
	if err != reporting.ErrAlreadySigned {
		t.Errorf("second Sign err = %v, want ErrAlreadySigned", err)
	}
}

func TestSignFrameworkRejectsInvalidSize(t *testing.T) {
	t.Parallel()
	h := newFrameworkHarness(t)

	var generated reporting.FrameworkReport
	if err := h.store.Tx(fwCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		r, err := h.repo.GenerateFramework(ctx, tx, reporting.GenerateFrameworkRequest{
			ProfileID:  "cp_FWA",
			SnapshotID: "fs_FWA",
		})
		generated = r
		return err
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	err := h.store.Tx(fwCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.SignFramework(ctx, tx, generated.ID, "key_X", []byte("short"), 1, "h", time.Now())
		return e
	})
	if err != reporting.ErrInvalidSignature {
		t.Errorf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestGetFrameworkReportNotFound(t *testing.T) {
	t.Parallel()
	h := newFrameworkHarness(t)
	err := h.store.Tx(fwCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.GetFrameworkReport(ctx, tx, "frep_DOES_NOT_EXIST")
		return e
	})
	if err != reporting.ErrFrameworkReportNotFound {
		t.Errorf("err = %v, want ErrFrameworkReportNotFound", err)
	}
}

func TestListFrameworkReportsReturnsDESC(t *testing.T) {
	t.Parallel()
	h := newFrameworkHarness(t)

	for i := 0; i < 3; i++ {
		if err := h.store.Tx(fwCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
			_, e := h.repo.GenerateFramework(ctx, tx, reporting.GenerateFrameworkRequest{
				ProfileID:  "cp_FWA",
				SnapshotID: "fs_FWA",
			})
			return e
		}); err != nil {
			t.Fatalf("Generate[%d]: %v", i, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	var reports []reporting.FrameworkReport
	if err := h.store.Tx(fwCtx(h.tenantID), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.repo.ListFrameworkReports(ctx, tx, reporting.FrameworkListFilter{})
		reports = out
		return e
	}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(reports) != 3 {
		t.Fatalf("len = %d, want 3", len(reports))
	}
	// generated_at DESC — 첫 번째가 가장 최근(0번째 마지막 생성).
	for i := 0; i < len(reports)-1; i++ {
		if reports[i].GeneratedAt.Before(reports[i+1].GeneratedAt) {
			t.Errorf("not DESC at index %d: %v < %v", i, reports[i].GeneratedAt, reports[i+1].GeneratedAt)
		}
	}
	// PDF body는 List에서 nil이어야 함.
	if reports[0].PDF != nil {
		t.Errorf("List should not include PDF body, got %d bytes", len(reports[0].PDF))
	}
}
