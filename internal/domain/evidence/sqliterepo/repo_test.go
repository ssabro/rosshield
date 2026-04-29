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
	"github.com/ssabro/rosshield/internal/domain/evidence"
	"github.com/ssabro/rosshield/internal/domain/evidence/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/blobstore"
	blobfs "github.com/ssabro/rosshield/internal/platform/blobstore/fs"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

const testTenant = "tnt_test"

// auditAdapter는 audit.Service를 evidence.AuditEmitter로 감싸는 테스트용 어댑터.
type auditAdapter struct{ svc audit.Service }

func (a *auditAdapter) EmitEvidenceStored(ctx context.Context, tx storage.Tx, rec evidence.Record) error {
	_, err := a.svc.Append(ctx, tx, audit.AppendRequest{
		TenantID: rec.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   "evidence.stored",
		Target:   audit.Target{Type: "evidence", ID: rec.ID},
		Payload:  []byte(`{"sha256":"` + rec.SHA256 + `"}`),
		Outcome:  audit.OutcomeSuccess,
	})
	return err
}

func newTestRepo(t *testing.T) (*sqliterepo.Repo, audit.Service, storage.Storage, blobstore.Store) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "evidence.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	bs, err := blobfs.New(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatalf("blobfs.New: %v", err)
	}

	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clock.System()})
	repo := sqliterepo.New(sqliterepo.Deps{
		Clock:     clock.System(),
		IDGen:     idgen.NewULID(),
		Audit:     &auditAdapter{svc: auditSvc},
		BlobStore: bs,
	})

	// 최소 tenant FK 시드.
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		_, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'test', 'desktop_free', ?)`,
			testTenant, now)
		return err
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	return repo, auditSvc, store, bs
}

func ctxWithTenant(t storage.TenantID) context.Context {
	return storage.WithTenantID(context.Background(), t)
}

// === Store ===

func TestStoreNewEvidenceInsertsAndReturnsIsNew(t *testing.T) {
	repo, _, store, _ := newTestRepo(t)
	ctx := ctxWithTenant(testTenant)

	var res evidence.StoreResult
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		res, err = repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: testTenant, ContentType: evidence.ContentStdout,
			Raw: []byte("hello world"),
		})
		return err
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if !res.IsNew {
		t.Fatalf("IsNew want true, got false")
	}
	if res.EvidenceID == "" || res.SHA256 == "" {
		t.Fatalf("empty IDs: %+v", res)
	}
	if res.SizeBytes != 11 {
		t.Fatalf("SizeBytes=%d, want 11", res.SizeBytes)
	}
}

func TestStoreDedupReturnsExistingIDIsNewFalse(t *testing.T) {
	repo, _, store, _ := newTestRepo(t)
	ctx := ctxWithTenant(testTenant)

	var first, second evidence.StoreResult
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		first, err = repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: testTenant, ContentType: evidence.ContentStdout,
			Raw: []byte("dedup-me"),
		})
		if err != nil {
			return err
		}
		second, err = repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: testTenant, ContentType: evidence.ContentStdout,
			Raw: []byte("dedup-me"),
		})
		return err
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if first.EvidenceID != second.EvidenceID {
		t.Fatalf("dedup failed: first=%q != second=%q", first.EvidenceID, second.EvidenceID)
	}
	if !first.IsNew || second.IsNew {
		t.Fatalf("IsNew flags: first=%v second=%v (want true,false)", first.IsNew, second.IsNew)
	}
}

func TestStoreRedactsSecretsBeforeHashing(t *testing.T) {
	repo, _, store, bs := newTestRepo(t)
	ctx := ctxWithTenant(testTenant)

	var res evidence.StoreResult
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		res, err = repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: testTenant, ContentType: evidence.ContentStdout,
			Raw: []byte("hello password=topsecret world"),
		})
		return err
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if len(res.Redactions) == 0 {
		t.Fatalf("expected at least one redaction mark")
	}

	// blob 내용은 redact된 형태(원문 'topsecret' 누출 없음).
	body, err := bs.Get(context.Background(), res.SHA256)
	if err != nil {
		t.Fatalf("blob get: %v", err)
	}
	if string(body) == "hello password=topsecret world" {
		t.Fatalf("blob contains plaintext secret — redaction missed")
	}
	// 마커 형식 `[REDACTED:password:N]`만 강제 — N(원본 길이)은 redaction.go가 결정.
	if !strings.Contains(string(body), "[REDACTED:password:") {
		t.Fatalf("blob missing password marker: %q", body)
	}
	if strings.Contains(string(body), "topsecret") {
		t.Fatalf("blob still contains 'topsecret': %q", body)
	}
}

func TestStoreEmitsAuditOnFirstWriteOnly(t *testing.T) {
	repo, audSvc, store, _ := newTestRepo(t)
	ctx := ctxWithTenant(testTenant)

	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: testTenant, ContentType: evidence.ContentStderr, Raw: []byte("a"),
		})
		return err
	}); err != nil {
		t.Fatalf("first Store: %v", err)
	}
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: testTenant, ContentType: evidence.ContentStderr, Raw: []byte("a"),
		})
		return err
	}); err != nil {
		t.Fatalf("second Store: %v", err)
	}

	// audit chain head는 1회만 — 두 번째 Store는 dedup 히트라 emit 없음.
	headSeq := mustHeadSeq(t, store, audSvc, testTenant)
	if headSeq != 1 {
		t.Fatalf("audit head=%d, want 1 (dedup은 emit X)", headSeq)
	}
}

func TestStoreRejectsInvalidContentType(t *testing.T) {
	repo, _, store, _ := newTestRepo(t)
	ctx := ctxWithTenant(testTenant)

	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: testTenant, ContentType: "weird", Raw: []byte("x"),
		})
		return err
	})
	if !errors.Is(err, evidence.ErrInvalidContentType) {
		t.Fatalf("err=%v, want ErrInvalidContentType", err)
	}
}

func TestStoreRejectsTenantMismatch(t *testing.T) {
	repo, _, store, _ := newTestRepo(t)
	ctx := ctxWithTenant(testTenant)

	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: "tnt_other", ContentType: evidence.ContentStdout, Raw: []byte("x"),
		})
		return err
	})
	if err == nil {
		t.Fatalf("expected tenant mismatch error")
	}
}

func TestStoreEmptyRawSucceeds(t *testing.T) {
	repo, _, store, _ := newTestRepo(t)
	ctx := ctxWithTenant(testTenant)

	var res evidence.StoreResult
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		res, err = repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: testTenant, ContentType: evidence.ContentStdout, Raw: nil,
		})
		return err
	})
	if err != nil {
		t.Fatalf("Store empty: %v", err)
	}
	// sha256("") = e3b0c44...
	const emptySHA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if res.SHA256 != emptySHA {
		t.Fatalf("sha=%q, want %s", res.SHA256, emptySHA)
	}
	if res.SizeBytes != 0 {
		t.Fatalf("SizeBytes=%d, want 0", res.SizeBytes)
	}
}

// === Read ===

func TestReadReturnsRecordAndDecryptedBlob(t *testing.T) {
	repo, _, store, _ := newTestRepo(t)
	ctx := ctxWithTenant(testTenant)

	var stored evidence.StoreResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		stored, err = repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: testTenant, ContentType: evidence.ContentStdout, Raw: []byte("readback me"),
		})
		return err
	}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	var (
		rec  evidence.Record
		body []byte
	)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		rec, body, err = repo.Read(ctx, tx, stored.EvidenceID)
		return err
	}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if rec.ID != stored.EvidenceID {
		t.Fatalf("rec.ID=%q, want %q", rec.ID, stored.EvidenceID)
	}
	if string(body) != "readback me" {
		t.Fatalf("body=%q", body)
	}
}

func TestReadReturnsErrEvidenceNotFoundForUnknownID(t *testing.T) {
	repo, _, store, _ := newTestRepo(t)
	ctx := ctxWithTenant(testTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, _, e := repo.Read(ctx, tx, "ev_unknown")
		return e
	})
	if !errors.Is(err, evidence.ErrEvidenceNotFound) {
		t.Fatalf("err=%v, want ErrEvidenceNotFound", err)
	}
}

// === LinkToResult / ListForResult ===

func TestLinkToResultInsertsRefsAndListReturnsThemInPositionOrder(t *testing.T) {
	repo, _, store, _ := newTestRepo(t)
	ctx := ctxWithTenant(testTenant)

	// scan_result row 시드(FK 만족) + evidence 3개 Store + Link.
	const scanResultID = "scr_fixture"
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		return seedScanResult(ctx, tx, scanResultID, testTenant)
	}); err != nil {
		t.Fatalf("seed scan_result: %v", err)
	}

	var ids []string
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		for _, body := range [][]byte{[]byte("first"), []byte("second"), []byte("third")} {
			r, err := repo.Store(ctx, tx, evidence.StoreInput{
				TenantID: testTenant, ContentType: evidence.ContentStdout, Raw: body,
			})
			if err != nil {
				return err
			}
			ids = append(ids, r.EvidenceID)
		}
		_, err := repo.LinkToResult(ctx, tx, scanResultID, ids)
		return err
	}); err != nil {
		t.Fatalf("Store/Link: %v", err)
	}

	var listed []evidence.Record
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		listed, err = repo.ListForResult(ctx, tx, scanResultID)
		return err
	}); err != nil {
		t.Fatalf("ListForResult: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("listed=%d, want 3", len(listed))
	}
	for i, rec := range listed {
		if rec.ID != ids[i] {
			t.Fatalf("position %d: rec.ID=%q, want %q", i, rec.ID, ids[i])
		}
	}
}

func TestLinkToResultIdempotentOnDuplicate(t *testing.T) {
	repo, _, store, _ := newTestRepo(t)
	ctx := ctxWithTenant(testTenant)
	const scanResultID = "scr_dup"
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		return seedScanResult(ctx, tx, scanResultID, testTenant)
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var evID string
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: testTenant, ContentType: evidence.ContentStdout, Raw: []byte("x"),
		})
		if err != nil {
			return err
		}
		evID = r.EvidenceID
		if _, err := repo.LinkToResult(ctx, tx, scanResultID, []string{evID}); err != nil {
			return err
		}
		// 다시 호출 — idempotent.
		_, err = repo.LinkToResult(ctx, tx, scanResultID, []string{evID})
		return err
	}); err != nil {
		t.Fatalf("Link 2회: %v", err)
	}

	// ListForResult는 1행만.
	var listed []evidence.Record
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		listed, err = repo.ListForResult(ctx, tx, scanResultID)
		return err
	}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed=%d, want 1 (idempotent)", len(listed))
	}
}

func TestLinkToResultRejectsEmptyEvidenceIDs(t *testing.T) {
	repo, _, store, _ := newTestRepo(t)
	ctx := ctxWithTenant(testTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.LinkToResult(ctx, tx, "scr_x", nil)
		return e
	})
	if !errors.Is(err, evidence.ErrEvidenceIDsEmpty) {
		t.Fatalf("err=%v, want ErrEvidenceIDsEmpty", err)
	}
}

// === Cross-tenant 격리 ===

func TestCrossTenantReadReturnsNotFound(t *testing.T) {
	repo, _, store, _ := newTestRepo(t)
	const otherTenant = "tnt_other"

	// 다른 tenant 시드.
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'other', 'desktop_free', ?)`,
			otherTenant, time.Now().UTC().Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatalf("seed other: %v", err)
	}

	var stored evidence.StoreResult
	if err := store.Tx(ctxWithTenant(testTenant), func(ctx context.Context, tx storage.Tx) error {
		var err error
		stored, err = repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: testTenant, ContentType: evidence.ContentStdout, Raw: []byte("secret-of-A"),
		})
		return err
	}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// otherTenant ctx로 같은 evidenceID 읽기 시도 → ErrEvidenceNotFound.
	err := store.Tx(ctxWithTenant(otherTenant), func(ctx context.Context, tx storage.Tx) error {
		_, _, e := repo.Read(ctx, tx, stored.EvidenceID)
		return e
	})
	if !errors.Is(err, evidence.ErrEvidenceNotFound) {
		t.Fatalf("err=%v, want ErrEvidenceNotFound (cross-tenant)", err)
	}
}

func TestCrossTenantSameSHADoesNotCollide(t *testing.T) {
	repo, _, store, _ := newTestRepo(t)
	const otherTenant = "tnt_other"
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'other', 'desktop_free', ?)`,
			otherTenant, time.Now().UTC().Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var (
		a, b evidence.StoreResult
	)
	if err := store.Tx(ctxWithTenant(testTenant), func(ctx context.Context, tx storage.Tx) error {
		var err error
		a, err = repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: testTenant, ContentType: evidence.ContentStdout, Raw: []byte("identical"),
		})
		return err
	}); err != nil {
		t.Fatalf("Store A: %v", err)
	}
	if err := store.Tx(ctxWithTenant(otherTenant), func(ctx context.Context, tx storage.Tx) error {
		var err error
		b, err = repo.Store(ctx, tx, evidence.StoreInput{
			TenantID: otherTenant, ContentType: evidence.ContentStdout, Raw: []byte("identical"),
		})
		return err
	}); err != nil {
		t.Fatalf("Store B: %v", err)
	}
	if a.SHA256 != b.SHA256 {
		t.Fatalf("same content → different sha? a=%s b=%s", a.SHA256, b.SHA256)
	}
	if a.EvidenceID == b.EvidenceID {
		t.Fatalf("cross-tenant evidence_id 같음 (격리 실패): %q", a.EvidenceID)
	}
	if !a.IsNew || !b.IsNew {
		t.Fatalf("IsNew: a=%v b=%v (want both true)", a.IsNew, b.IsNew)
	}
}

// --- helpers ---

// seedScanResult는 evidence_refs FK를 만족시키기 위해 scan_result 1행을 raw INSERT합니다.
// (E5 패턴 — 도메인 의존 회피로 격리 단순화.)
func seedScanResult(ctx context.Context, tx storage.Tx, id string, tenantID storage.TenantID) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// 의존 FK들도 미리 시드.
	if _, err := tx.Exec(ctx, `INSERT OR IGNORE INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at)
VALUES ('fl_seed', ?, 'fleet-seed', '', '{}', ?, ?)`, string(tenantID), now, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT OR IGNORE INTO packs (id, tenant_id, name, version, vendor, pack_key,
    manifest_hash, signer_key_id, installed_at)
VALUES ('pk_seed', ?, 'cis', '1.0', 'rs', 'rs-cis-1.0', x'00', 'key_seed', ?)`,
		string(tenantID), now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT OR IGNORE INTO pack_checks (id, pack_id, check_id, title, severity,
    audit_command, evaluation_rule, rationale, fix_guidance)
VALUES ('ck_seed', 'pk_seed', 'CHK-1', 't', 'medium', 'true', '{}', '', '')`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT OR IGNORE INTO credentials (id, tenant_id, type, encrypted_payload, encryption_meta, created_at, updated_at)
VALUES ('cr_seed', ?, 'password', x'00', '{}', ?, ?)`, string(tenantID), now, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT OR IGNORE INTO robots (id, tenant_id, fleet_id, credential_id, name, host, port,
    auth_type, criticality, tags, created_at, updated_at)
VALUES ('rb_seed', ?, 'fl_seed', 'cr_seed', 'robot-seed', 'localhost', 22, 'password',
    'medium', '[]', ?, ?)`, string(tenantID), now, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT OR IGNORE INTO scan_sessions (id, tenant_id, fleet_id, pack_id, trigger,
    status, progress_total, progress_completed, progress_failed, failure_reason,
    created_at, updated_at, started_at, completed_at)
VALUES ('scan_seed', ?, 'fl_seed', 'pk_seed', 'manual', 'running', 1, 0, 0, '',
    ?, ?, ?, NULL)`,
		string(tenantID), now, now, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO scan_results (id, session_id, tenant_id, robot_id, check_id, pack_check_id,
    outcome, eval_reason, duration_ms, executed_at, created_at)
VALUES (?, 'scan_seed', ?, 'rb_seed', 'CHK-1', 'ck_seed', 'pass', '', 100, ?, ?)`,
		id, string(tenantID), now, now); err != nil {
		return err
	}
	return nil
}

func mustHeadSeq(t *testing.T, store storage.Storage, svc audit.Service, tenantID storage.TenantID) int64 {
	t.Helper()
	var seq int64
	if err := store.Tx(ctxWithTenant(tenantID), func(ctx context.Context, tx storage.Tx) error {
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
