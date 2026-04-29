package sqliterepo_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/reporting"
	"github.com/ssabro/rosshield/internal/domain/reporting/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

const (
	testTenant     = "tn_rep"
	otherTenant    = "tn_other"
	testFleet      = "fl_rep"
	testPack       = "pk_rep"
	testRobot      = "rb_rep"
	testCheck      = "ck_rep"
	testSession    = "scan_rep"
	otherSession   = "scan_rep2"
	completedSess  = "scan_done"
	pendingSess    = "scan_pending"
	completedSess2 = "scan_done2"
)

// === fakes ===

// fakeBuilder는 PDFInput을 보존하고 고정 bytes(또는 동적 함수)를 반환합니다.
type fakeBuilder struct {
	output  []byte // nil이면 buildFn 사용
	buildFn func(input reporting.PDFInput) ([]byte, error)
	last    reporting.PDFInput
	calls   atomic.Int32
	err     error
}

func (f *fakeBuilder) Build(input reporting.PDFInput) ([]byte, error) {
	f.calls.Add(1)
	f.last = input
	if f.err != nil {
		return nil, f.err
	}
	if f.buildFn != nil {
		return f.buildFn(input)
	}
	out := f.output
	if out == nil {
		out = []byte("PDF-FAKE-DEFAULT")
	}
	return append([]byte(nil), out...), nil
}

// fakeScan은 Generate가 필요한 ScanReader를 fake-implements합니다.
type fakeScan struct {
	sessions map[string]sqliterepo.ScanSessionView
	results  map[string][]sqliterepo.ScanResultView
}

func (f *fakeScan) GetSession(_ context.Context, _ storage.Tx, id string) (sqliterepo.ScanSessionView, error) {
	s, ok := f.sessions[id]
	if !ok {
		return sqliterepo.ScanSessionView{}, storage.ErrNotFound
	}
	return s, nil
}

func (f *fakeScan) ListResults(_ context.Context, _ storage.Tx, sessionID string) ([]sqliterepo.ScanResultView, error) {
	return f.results[sessionID], nil
}

// fakeEvidence는 ScanResultID → SHA 슬라이스를 매핑합니다.
type fakeEvidence struct {
	byResult map[string][]string
}

func (f *fakeEvidence) ListForResult(_ context.Context, _ storage.Tx, scanResultID string) ([]sqliterepo.EvidenceView, error) {
	out := make([]sqliterepo.EvidenceView, 0, len(f.byResult[scanResultID]))
	for _, sha := range f.byResult[scanResultID] {
		out = append(out, sqliterepo.EvidenceView{SHA256: sha})
	}
	return out, nil
}

// fakeTenant는 GetTenant fake.
type fakeTenant struct {
	byID map[storage.TenantID]string // ID → Name
}

func (f *fakeTenant) GetTenant(_ context.Context, _ storage.Tx, id storage.TenantID) (sqliterepo.TenantView, error) {
	name, ok := f.byID[id]
	if !ok {
		return sqliterepo.TenantView{}, storage.ErrNotFound
	}
	return sqliterepo.TenantView{ID: id, Name: name}, nil
}

// auditAdapter는 audit.Service를 reporting.AuditEmitter로 감싸는 테스트 어댑터.
type auditAdapter struct{ svc audit.Service }

func (a *auditAdapter) EmitReportGenerated(ctx context.Context, tx storage.Tx, r reporting.Report) error {
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: r.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "report.generated",
		Target:   audit.Target{Type: "report", ID: r.ID},
		Payload:  []byte(`{"sha256":"` + r.PDFSHA256 + `"}`),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

func (a *auditAdapter) EmitReportSigned(ctx context.Context, tx storage.Tx, r reporting.Report) error {
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: r.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "report.signed",
		Target:   audit.Target{Type: "report", ID: r.ID},
		Payload:  []byte(`{"keyId":"` + r.Signature.SignerKeyID + `"}`),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

// === harness ===

type harness struct {
	repo     *sqliterepo.Repo
	store    storage.Storage
	auditSvc audit.Service
	scan     *fakeScan
	ev       *fakeEvidence
	tenant   *fakeTenant
	builder  *fakeBuilder
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "reporting.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clock.System()})
	scan := &fakeScan{sessions: map[string]sqliterepo.ScanSessionView{}, results: map[string][]sqliterepo.ScanResultView{}}
	ev := &fakeEvidence{byResult: map[string][]string{}}
	tenantR := &fakeTenant{byID: map[storage.TenantID]string{}}
	builder := &fakeBuilder{output: []byte("PDF-DEFAULT-OUTPUT")}

	repo := sqliterepo.New(sqliterepo.Deps{
		Clock:    clock.System(),
		IDGen:    idgen.NewULID(),
		Audit:    &auditAdapter{svc: auditSvc},
		Builder:  builder,
		Scan:     scan,
		Evidence: ev,
		Tenant:   tenantR,
	})

	// 최소 tenant FK 시드 + scan_session/result FK 만족용 raw INSERT.
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'rep-test', 'desktop_free', ?)`,
			testTenant, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'other', 'desktop_free', ?)`,
			otherTenant, now); err != nil {
			return err
		}
		return seedScanSessionsForReports(ctx, tx, now)
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tenantR.byID[testTenant] = "Rep Test Tenant"
	tenantR.byID[otherTenant] = "Other Tenant"

	return &harness{
		repo:     repo,
		store:    store,
		auditSvc: auditSvc,
		scan:     scan,
		ev:       ev,
		tenant:   tenantR,
		builder:  builder,
	}
}

func ctxFor(tenant string) context.Context {
	return storage.WithTenantID(context.Background(), storage.TenantID(tenant))
}

// seedScanSessionsForReports는 reports.scope_session_id FK를 만족시키기 위해
// scan_sessions 행 몇 개를 raw INSERT합니다 (실 도메인 격리).
func seedScanSessionsForReports(ctx context.Context, tx storage.Tx, now string) error {
	// fleets / packs / pack_checks / credentials / robots — FK 체인 만족.
	if _, err := tx.Exec(ctx, `INSERT INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at)
VALUES (?, ?, 'fleet-rep', '', '{}', ?, ?)`, testFleet, testTenant, now, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO packs (id, tenant_id, name, version, vendor, pack_key,
    manifest_hash, signer_key_id, installed_at)
VALUES (?, ?, 'cis', '1.0', 'rs', 'rs-cis-1.0', x'00', 'key_seed', ?)`,
		testPack, testTenant, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO pack_checks (id, pack_id, check_id, title, severity,
    audit_command, evaluation_rule, rationale, fix_guidance)
VALUES (?, ?, 'CIS-1.1.1.1', 't', 'medium', 'true', '{}', '', '')`,
		testCheck, testPack); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO credentials (id, tenant_id, type, encrypted_payload, encryption_meta, created_at, updated_at)
VALUES ('cr_rep', ?, 'password', x'00', '{}', ?, ?)`, testTenant, now, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO robots (id, tenant_id, fleet_id, credential_id, name, host, port,
    auth_type, criticality, tags, created_at, updated_at)
VALUES (?, ?, ?, 'cr_rep', 'robot-rep', 'localhost', 22, 'password',
    'medium', '[]', ?, ?)`, testRobot, testTenant, testFleet, now, now); err != nil {
		return err
	}
	for _, sid := range []string{testSession, otherSession, completedSess, pendingSess, completedSess2} {
		if _, err := tx.Exec(ctx, `INSERT INTO scan_sessions (id, tenant_id, fleet_id, pack_id, trigger,
    status, progress_total, progress_completed, progress_failed, failure_reason,
    created_at, updated_at, started_at, completed_at)
VALUES (?, ?, ?, ?, 'manual', 'completed', 1, 1, 0, '',
    ?, ?, ?, ?)`,
			sid, testTenant, testFleet, testPack, now, now, now, now); err != nil {
			return err
		}
	}
	return nil
}

func setupCompletedSession(h *harness, sessionID string, results []sqliterepo.ScanResultView) {
	h.scan.sessions[sessionID] = sqliterepo.ScanSessionView{
		ID: sessionID, TenantID: testTenant, FleetID: testFleet, PackID: testPack,
		Status: "completed",
	}
	h.scan.results[sessionID] = results
}

// === Tests — Generate ===

func TestGenerateBuildsPDFAndInsertsRow(t *testing.T) {
	h := newHarness(t)
	setupCompletedSession(h, completedSess, []sqliterepo.ScanResultView{
		{ID: "scr_a", RobotID: testRobot, CheckID: "CHK-1", Outcome: "pass"},
	})

	var rep reporting.Report
	if err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		var err error
		rep, err = h.repo.Generate(ctx, tx, reporting.GenerateRequest{
			TenantID:    testTenant,
			SessionID:   completedSess,
			GeneratedBy: "user_a",
			GeneratedAt: time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC),
		})
		return err
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if rep.ID == "" {
		t.Fatalf("empty report ID")
	}
	if rep.TenantID != testTenant {
		t.Fatalf("tenant=%q", rep.TenantID)
	}
	if string(rep.ScopeType) != "session" {
		t.Fatalf("scope=%q", rep.ScopeType)
	}
	if rep.Format != "pdf" {
		t.Fatalf("format=%q", rep.Format)
	}
	if !bytes.Equal(rep.PDF, []byte("PDF-DEFAULT-OUTPUT")) {
		t.Fatalf("pdf body mismatch: %q", rep.PDF)
	}
	if rep.PDFSizeBytes != int64(len(rep.PDF)) {
		t.Fatalf("size=%d, want %d", rep.PDFSizeBytes, len(rep.PDF))
	}
	if rep.PDFSHA256 == "" || len(rep.PDFSHA256) != 64 {
		t.Fatalf("sha256=%q", rep.PDFSHA256)
	}
	if !rep.Signature.IsZero() {
		t.Fatalf("Generate must leave signature zero, got %+v", rep.Signature)
	}

	// reports row 1행 확인.
	var count int
	if err := h.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		return tx.QueryRow(ctx, `SELECT COUNT(*) FROM reports WHERE id = ?`, rep.ID).Scan(&count)
	}); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows=%d, want 1", count)
	}
}

func TestGenerateRejectsNonCompletedSession(t *testing.T) {
	h := newHarness(t)
	h.scan.sessions[pendingSess] = sqliterepo.ScanSessionView{
		ID: pendingSess, TenantID: testTenant, Status: "pending",
	}

	err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.Generate(ctx, tx, reporting.GenerateRequest{
			TenantID: testTenant, SessionID: pendingSess, GeneratedBy: "user",
		})
		return e
	})
	if !errors.Is(err, reporting.ErrSessionNotCompleted) {
		t.Fatalf("err=%v, want ErrSessionNotCompleted", err)
	}
}

func TestGenerateRejectsMissingSessionID(t *testing.T) {
	h := newHarness(t)
	err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.Generate(ctx, tx, reporting.GenerateRequest{
			TenantID: testTenant, SessionID: "  ",
		})
		return e
	})
	if !errors.Is(err, reporting.ErrSessionMissing) {
		t.Fatalf("err=%v, want ErrSessionMissing", err)
	}
}

func TestGenerateUsesProvidedGeneratedAtForDeterminism(t *testing.T) {
	h := newHarness(t)
	setupCompletedSession(h, completedSess, []sqliterepo.ScanResultView{
		{ID: "scr_x", RobotID: testRobot, CheckID: "CHK-1", Outcome: "pass"},
	})
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	var rep reporting.Report
	if err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		var err error
		rep, err = h.repo.Generate(ctx, tx, reporting.GenerateRequest{
			TenantID: testTenant, SessionID: completedSess, GeneratedAt: want, GeneratedBy: "u",
		})
		return err
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !rep.GeneratedAt.Equal(want) {
		t.Fatalf("generatedAt=%v, want %v", rep.GeneratedAt, want)
	}
	if !h.builder.last.GeneratedAt.Equal(want) {
		t.Fatalf("builder input GeneratedAt=%v, want %v", h.builder.last.GeneratedAt, want)
	}
}

func TestGenerateAggregatesStatsCorrectly(t *testing.T) {
	h := newHarness(t)
	results := []sqliterepo.ScanResultView{}
	for i := 0; i < 5; i++ {
		results = append(results, sqliterepo.ScanResultView{ID: fmt.Sprintf("scr_p%d", i), RobotID: testRobot, CheckID: "p", Outcome: "pass"})
	}
	for i := 0; i < 3; i++ {
		results = append(results, sqliterepo.ScanResultView{ID: fmt.Sprintf("scr_f%d", i), RobotID: testRobot, CheckID: "f", Outcome: "fail"})
	}
	results = append(results, sqliterepo.ScanResultView{ID: "scr_e", RobotID: testRobot, CheckID: "e", Outcome: "error"})
	setupCompletedSession(h, completedSess, results)

	if err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, err := h.repo.Generate(ctx, tx, reporting.GenerateRequest{
			TenantID: testTenant, SessionID: completedSess, GeneratedBy: "u",
		})
		return err
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	stats := h.builder.last.Stats
	if stats.TotalChecks != 9 || stats.Pass != 5 || stats.Fail != 3 || stats.Error != 1 {
		t.Fatalf("stats=%+v, want total=9 pass=5 fail=3 error=1", stats)
	}
}

func TestGenerateRowsSortedByRobotThenCheck(t *testing.T) {
	h := newHarness(t)
	// 의도적으로 뒤섞인 순서로 입력.
	setupCompletedSession(h, completedSess, []sqliterepo.ScanResultView{
		{ID: "scr_1", RobotID: "rb_b", CheckID: "CHK-2", Outcome: "pass"},
		{ID: "scr_2", RobotID: "rb_a", CheckID: "CHK-2", Outcome: "pass"},
		{ID: "scr_3", RobotID: "rb_b", CheckID: "CHK-1", Outcome: "fail"},
		{ID: "scr_4", RobotID: "rb_a", CheckID: "CHK-1", Outcome: "pass"},
	})

	if err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, err := h.repo.Generate(ctx, tx, reporting.GenerateRequest{
			TenantID: testTenant, SessionID: completedSess, GeneratedBy: "u",
		})
		return err
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	rows := h.builder.last.Rows
	want := [][2]string{
		{"rb_a", "CHK-1"}, {"rb_a", "CHK-2"}, {"rb_b", "CHK-1"}, {"rb_b", "CHK-2"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows=%d, want %d", len(rows), len(want))
	}
	for i, w := range want {
		if rows[i].RobotID != w[0] || rows[i].CheckCode != w[1] {
			t.Fatalf("row[%d]=(%s,%s), want (%s,%s)", i, rows[i].RobotID, rows[i].CheckCode, w[0], w[1])
		}
	}
}

func TestGenerateAttachesEvidenceSHAs(t *testing.T) {
	h := newHarness(t)
	setupCompletedSession(h, completedSess, []sqliterepo.ScanResultView{
		{ID: "scr_e1", RobotID: testRobot, CheckID: "CHK-1", Outcome: "fail"},
	})
	h.ev.byResult["scr_e1"] = []string{"sha-aaa", "sha-bbb"}

	if err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, err := h.repo.Generate(ctx, tx, reporting.GenerateRequest{
			TenantID: testTenant, SessionID: completedSess, GeneratedBy: "u",
		})
		return err
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(h.builder.last.Rows) != 1 {
		t.Fatalf("rows=%d", len(h.builder.last.Rows))
	}
	got := h.builder.last.Rows[0].EvidenceSHAs
	if len(got) != 2 || got[0] != "sha-aaa" || got[1] != "sha-bbb" {
		t.Fatalf("evidence shas=%v", got)
	}
}

func TestGenerateBuilderErrorPropagates(t *testing.T) {
	h := newHarness(t)
	setupCompletedSession(h, completedSess, []sqliterepo.ScanResultView{
		{ID: "scr", RobotID: testRobot, CheckID: "CHK-1", Outcome: "pass"},
	})
	h.builder.err = errors.New("boom")

	err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.Generate(ctx, tx, reporting.GenerateRequest{
			TenantID: testTenant, SessionID: completedSess, GeneratedBy: "u",
		})
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v, want builder error propagated", err)
	}
}

func TestGenerateBuilderNilReturnsErr(t *testing.T) {
	h := newHarness(t)
	// Builder를 nil로 — 새 repo 인스턴스(다른 deps).
	repoNil := sqliterepo.New(sqliterepo.Deps{
		Clock: clock.System(), IDGen: idgen.NewULID(),
		Scan: h.scan, Evidence: h.ev, Tenant: h.tenant,
	})
	setupCompletedSession(h, completedSess, []sqliterepo.ScanResultView{
		{ID: "scr", RobotID: testRobot, CheckID: "CHK-1", Outcome: "pass"},
	})

	err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := repoNil.Generate(ctx, tx, reporting.GenerateRequest{
			TenantID: testTenant, SessionID: completedSess,
		})
		return e
	})
	if !errors.Is(err, reporting.ErrBuilderNil) {
		t.Fatalf("err=%v, want ErrBuilderNil", err)
	}
}

// === Sign ===

func TestSignAttachesSignatureAndUpdatesRow(t *testing.T) {
	h := newHarness(t)
	rep := generateOne(t, h, completedSess)

	sig := bytes.Repeat([]byte{0xAB}, 64)
	signedAt := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	var signed reporting.Report
	if err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		var err error
		signed, err = h.repo.Sign(ctx, tx, rep.ID, "key_test123", sig, 42, "abc123def456", signedAt)
		return err
	}); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if signed.Signature.IsZero() {
		t.Fatalf("signature still zero after Sign")
	}
	if signed.Signature.SignerKeyID != "key_test123" {
		t.Fatalf("keyID=%q", signed.Signature.SignerKeyID)
	}
	if !bytes.Equal(signed.Signature.Signature, sig) {
		t.Fatalf("sig bytes mismatch")
	}
	if signed.Signature.ChainHeadSeq != 42 {
		t.Fatalf("seq=%d", signed.Signature.ChainHeadSeq)
	}
	if signed.Signature.ChainHeadHash != "abc123def456" {
		t.Fatalf("hash=%q", signed.Signature.ChainHeadHash)
	}

	// DB row 직접 확인.
	var (
		dbKey, dbHash string
		dbSig         []byte
		dbSeq         int64
	)
	if err := h.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT sig_key_id, sig_chain_head_hash, sig_bytes, sig_chain_head_seq FROM reports WHERE id = ?`,
			rep.ID,
		).Scan(&dbKey, &dbHash, &dbSig, &dbSeq)
	}); err != nil {
		t.Fatalf("db row: %v", err)
	}
	if dbKey != "key_test123" || dbHash != "abc123def456" || dbSeq != 42 || !bytes.Equal(dbSig, sig) {
		t.Fatalf("db row mismatch: key=%q hash=%q seq=%d sig=%x", dbKey, dbHash, dbSeq, dbSig)
	}
}

func TestSignRejectsAlreadySigned(t *testing.T) {
	h := newHarness(t)
	rep := generateOne(t, h, completedSess)
	sig := bytes.Repeat([]byte{0x01}, 64)

	if err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.Sign(ctx, tx, rep.ID, "key", sig, 1, "h", time.Now().UTC())
		return e
	}); err != nil {
		t.Fatalf("first Sign: %v", err)
	}
	err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.Sign(ctx, tx, rep.ID, "key", sig, 1, "h", time.Now().UTC())
		return e
	})
	if !errors.Is(err, reporting.ErrAlreadySigned) {
		t.Fatalf("err=%v, want ErrAlreadySigned", err)
	}
}

func TestSignRejectsInvalidSignatureSize(t *testing.T) {
	h := newHarness(t)
	rep := generateOne(t, h, completedSess)

	err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.Sign(ctx, tx, rep.ID, "key", make([]byte, 32), 1, "h", time.Now().UTC())
		return e
	})
	if !errors.Is(err, reporting.ErrInvalidSignature) {
		t.Fatalf("err=%v, want ErrInvalidSignature", err)
	}
}

func TestSignRejectsUnknownReportID(t *testing.T) {
	h := newHarness(t)
	err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.Sign(ctx, tx, "rep_unknown", "key", make([]byte, 64), 1, "h", time.Now().UTC())
		return e
	})
	if !errors.Is(err, reporting.ErrReportNotFound) {
		t.Fatalf("err=%v, want ErrReportNotFound", err)
	}
}

// === Get / List ===

func TestGetReportReturnsPDFBytes(t *testing.T) {
	h := newHarness(t)
	h.builder.output = []byte("PDF-CUSTOM-BODY-1234")
	rep := generateOne(t, h, completedSess)

	var got reporting.Report
	if err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		var err error
		got, err = h.repo.GetReport(ctx, tx, rep.ID)
		return err
	}); err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	if !bytes.Equal(got.PDF, []byte("PDF-CUSTOM-BODY-1234")) {
		t.Fatalf("pdf=%q", got.PDF)
	}
	if got.PDFSHA256 != rep.PDFSHA256 {
		t.Fatalf("sha mismatch: got=%q want=%q", got.PDFSHA256, rep.PDFSHA256)
	}
}

func TestListReportsReturnsMetaWithoutPDF(t *testing.T) {
	h := newHarness(t)
	r1 := generateOne(t, h, completedSess)
	// 두 번째 리포트 — 다른 session.
	setupCompletedSession(h, completedSess2, []sqliterepo.ScanResultView{
		{ID: "scr_z", RobotID: testRobot, CheckID: "z", Outcome: "pass"},
	})
	time.Sleep(2 * time.Millisecond) // generated_at 분리 보장.
	r2 := generateOne(t, h, completedSess2)

	var listed []reporting.Report
	if err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		var err error
		listed, err = h.repo.ListReports(ctx, tx, reporting.ListFilter{})
		return err
	}); err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed=%d, want 2", len(listed))
	}
	for _, rep := range listed {
		if rep.PDF != nil {
			t.Fatalf("ListReports must return PDF=nil, got %d bytes", len(rep.PDF))
		}
	}
	// generated_at DESC — r2가 먼저(가장 최근).
	_ = r1
	if listed[0].ID != r2.ID {
		t.Fatalf("first=%q, want most-recent %q", listed[0].ID, r2.ID)
	}
}

func TestListReportsSessionFilter(t *testing.T) {
	h := newHarness(t)
	r1 := generateOne(t, h, completedSess)
	setupCompletedSession(h, completedSess2, []sqliterepo.ScanResultView{
		{ID: "scr_zz", RobotID: testRobot, CheckID: "z", Outcome: "pass"},
	})
	_ = generateOne(t, h, completedSess2)

	var listed []reporting.Report
	if err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		var err error
		listed, err = h.repo.ListReports(ctx, tx, reporting.ListFilter{SessionID: completedSess})
		return err
	}); err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != r1.ID {
		t.Fatalf("filter mismatch: %+v", listed)
	}
}

// === Cross-tenant 격리 ===

func TestCrossTenantReportsIsolated(t *testing.T) {
	h := newHarness(t)
	rep := generateOne(t, h, completedSess)

	err := h.store.Tx(ctxFor(otherTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.GetReport(ctx, tx, rep.ID)
		return e
	})
	if !errors.Is(err, reporting.ErrReportNotFound) {
		t.Fatalf("err=%v, want ErrReportNotFound (cross-tenant)", err)
	}
}

// === Audit emit ===

func TestGenerateAuditEmitOnSuccess(t *testing.T) {
	h := newHarness(t)
	headBefore := mustHeadSeq(t, h.store, h.auditSvc, testTenant)
	_ = generateOne(t, h, completedSess)
	headAfter := mustHeadSeq(t, h.store, h.auditSvc, testTenant)
	if headAfter != headBefore+1 {
		t.Fatalf("audit head: before=%d after=%d, want +1", headBefore, headAfter)
	}
}

func TestSignAuditEmitOnSuccess(t *testing.T) {
	h := newHarness(t)
	rep := generateOne(t, h, completedSess)
	headBefore := mustHeadSeq(t, h.store, h.auditSvc, testTenant)

	sig := bytes.Repeat([]byte{0x42}, 64)
	if err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := h.repo.Sign(ctx, tx, rep.ID, "key", sig, 1, "h", time.Now().UTC())
		return e
	}); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	headAfter := mustHeadSeq(t, h.store, h.auditSvc, testTenant)
	if headAfter != headBefore+1 {
		t.Fatalf("audit head: before=%d after=%d, want +1", headBefore, headAfter)
	}
}

// === helpers ===

func generateOne(t *testing.T, h *harness, sessionID string) reporting.Report {
	t.Helper()
	if _, ok := h.scan.sessions[sessionID]; !ok {
		setupCompletedSession(h, sessionID, []sqliterepo.ScanResultView{
			{ID: "scr_default_" + sessionID, RobotID: testRobot, CheckID: "CHK-1", Outcome: "pass"},
		})
	}
	var rep reporting.Report
	if err := h.store.Tx(ctxFor(testTenant), func(ctx context.Context, tx storage.Tx) error {
		var err error
		rep, err = h.repo.Generate(ctx, tx, reporting.GenerateRequest{
			TenantID:  testTenant,
			SessionID: sessionID, GeneratedBy: "u",
		})
		return err
	}); err != nil {
		t.Fatalf("generateOne: %v", err)
	}
	return rep
}

func mustHeadSeq(t *testing.T, store storage.Storage, svc audit.Service, tenantID storage.TenantID) int64 {
	t.Helper()
	var seq int64
	if err := store.Tx(ctxFor(string(tenantID)), func(ctx context.Context, tx storage.Tx) error {
		head, err := svc.Head(ctx, tx, tenantID)
		if err != nil {
			return err
		}
		seq = head.Seq
		return nil
	}); err != nil {
		t.Fatalf("audit head: %v", err)
	}
	return seq
}
