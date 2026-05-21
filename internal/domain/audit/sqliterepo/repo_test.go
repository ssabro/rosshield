package sqliterepo_test

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

const testTenant storage.TenantID = "tn_test"

func newTestRepo(t *testing.T) (*sqliterepo.Repo, storage.Storage) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	repo := sqliterepo.New(sqliterepo.Deps{Clock: clock.System()})
	return repo, store
}

func sampleReq(action string) audit.AppendRequest {
	return audit.AppendRequest{
		TenantID: testTenant,
		Actor:    audit.Actor{Type: audit.ActorUser, ID: "us_test"},
		Action:   action,
		Target:   audit.Target{Type: "robot", ID: "ro_test"},
		Payload:  []byte(`{"name":"r1"}`),
		Outcome:  audit.OutcomeSuccess,
	}
}

func appendOne(t *testing.T, store storage.Storage, repo *sqliterepo.Repo, req audit.AppendRequest) audit.Entry {
	t.Helper()
	var out audit.Entry
	ctx := storage.WithTenantID(context.Background(), req.TenantID)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		e, err := repo.Append(ctx, tx, req)
		if err != nil {
			return err
		}
		out = e
		return nil
	})
	if err != nil {
		t.Fatalf("Tx/Append: %v", err)
	}
	return out
}

// E25 Stage 2 — RoleProvider gate (HA leader-only).

type fakeRole struct {
	leader bool
	epoch  int64
}

func (f *fakeRole) IsLeader() bool      { return f.leader }
func (f *fakeRole) CurrentEpoch() int64 { return f.epoch }

// 기본 동작(RoleProvider nil)에서 LeaderEpoch는 nil로 INSERT됨 (HA 비활성).
func TestAppendNoRoleProviderProducesNullEpoch(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)
	entry := appendOne(t, store, repo, sampleReq("compat.action"))
	if entry.LeaderEpoch != nil {
		t.Errorf("LeaderEpoch = %d, want nil (HA disabled)", *entry.LeaderEpoch)
	}
}

// HA 활성 + leader → LeaderEpoch 자동 채움 + INSERT 성공.
func TestAppendLeaderRoleRecordsEpoch(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	role := &fakeRole{leader: true, epoch: 42}
	repo := sqliterepo.New(sqliterepo.Deps{Clock: clock.System(), Role: role})

	entry := appendOne(t, store, repo, sampleReq("ha.action"))
	if entry.LeaderEpoch == nil {
		t.Fatal("LeaderEpoch is nil, want 42")
	}
	if *entry.LeaderEpoch != 42 {
		t.Errorf("LeaderEpoch = %d, want 42", *entry.LeaderEpoch)
	}
}

// HA 활성 + follower → ErrNotLeader, INSERT 차단.
func TestAppendFollowerRoleReturnsErrNotLeader(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	role := &fakeRole{leader: false}
	repo := sqliterepo.New(sqliterepo.Deps{Clock: clock.System(), Role: role})

	ctx := storage.WithTenantID(context.Background(), testTenant)
	err = store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.Append(ctx, tx, sampleReq("blocked.action"))
		return e
	})
	if !errors.Is(err, audit.ErrNotLeader) {
		t.Fatalf("expected ErrNotLeader, got %v", err)
	}
}

// E2.T1
func TestAppendInitializesGenesis(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	entry := appendOne(t, store, repo, sampleReq("robot.create"))

	if entry.Seq != 1 {
		t.Errorf("Seq = %d, want 1", entry.Seq)
	}
	if !entry.PrevHash.IsZero() {
		t.Errorf("first entry PrevHash = %x, want zero", entry.PrevHash)
	}
	if entry.Hash.IsZero() {
		t.Error("entry Hash should not be zero")
	}
	if entry.OccurredAt.IsZero() {
		t.Error("OccurredAt should be set by Service")
	}
	if entry.PayloadDigest == (audit.Hash{}) {
		t.Error("PayloadDigest should be sha256 of payload, not zero")
	}
}

// E2.T2
func TestAppendChainsHashes(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	first := appendOne(t, store, repo, sampleReq("robot.create"))
	second := appendOne(t, store, repo, sampleReq("robot.update"))

	if second.Seq != 2 {
		t.Errorf("second.Seq = %d, want 2", second.Seq)
	}
	if second.PrevHash != first.Hash {
		t.Errorf("second.PrevHash = %x, want %x (= first.Hash)", second.PrevHash, first.Hash)
	}

	expected, err := audit.ComputeEntryHash(second.PrevHash, second.PayloadDigest, second)
	if err != nil {
		t.Fatalf("ComputeEntryHash: %v", err)
	}
	if expected != second.Hash {
		t.Errorf("second.Hash mismatch:\n got  %x\n want %x", second.Hash, expected)
	}

	// Head 갱신 검증.
	ctx := storage.WithTenantID(context.Background(), testTenant)
	var head audit.ChainHead
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		h, err := repo.Head(ctx, tx, testTenant)
		head = h
		return err
	}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Seq != 2 || head.Hash != second.Hash {
		t.Errorf("head = (seq=%d, hash=%x), want (seq=2, hash=%x)", head.Seq, head.Hash, second.Hash)
	}
}

// E2.T3 — 직접 raw INSERT로 같은 (tenant_id, seq) 시도 → UNIQUE 위반.
// (정상 Append 경로는 head로 자동 단조 — 우회 시도가 막히는지 검증.)
func TestAppendRejectsDuplicateSeq(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	first := appendOne(t, store, repo, sampleReq("robot.create"))

	// 같은 seq로 raw INSERT → 실패해야 함.
	ctx := storage.WithTenantID(context.Background(), testTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `
INSERT INTO audit_entries (
    tenant_id, seq, occurred_at,
    actor_type, actor_id, actor_ip, actor_ua,
    action, target_type, target_id,
    payload_digest, outcome, error_code, error_message,
    prev_hash, hash
) VALUES (?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)`,
			string(testTenant), first.Seq, first.OccurredAt.Format(time.RFC3339Nano),
			"user", "us_dup",
			"robot.create", "robot", "ro_test",
			make([]byte, audit.HashSize), "success",
			make([]byte, audit.HashSize), make([]byte, audit.HashSize))
		return e
	})
	if err == nil {
		t.Fatal("expected UNIQUE violation for duplicate (tenant_id, seq)")
	}
}

// E2.T4
func TestAppendIsAppendOnly(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	first := appendOne(t, store, repo, sampleReq("robot.create"))
	ctx := storage.WithTenantID(context.Background(), testTenant)

	// UPDATE 시도 → trigger ABORT → ErrImmutable.
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE audit_entries SET action = 'tampered' WHERE tenant_id = ? AND seq = ?`,
			string(testTenant), first.Seq)
		return e
	})
	if !errors.Is(err, storage.ErrImmutable) {
		t.Errorf("UPDATE: err = %v, want ErrImmutable", err)
	}

	// DELETE 시도 → trigger ABORT → ErrImmutable.
	err = store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM audit_entries WHERE tenant_id = ? AND seq = ?`,
			string(testTenant), first.Seq)
		return e
	})
	if !errors.Is(err, storage.ErrImmutable) {
		t.Errorf("DELETE: err = %v, want ErrImmutable", err)
	}
}

// 보조: Head 미존재 → genesis 반환.
func TestHeadEmptyReturnsGenesis(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var head audit.ChainHead
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		h, err := repo.Head(ctx, tx, testTenant)
		head = h
		return err
	}); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Seq != 0 {
		t.Errorf("genesis Seq = %d, want 0", head.Seq)
	}
	if !head.Hash.IsZero() {
		t.Errorf("genesis Hash = %x, want zero", head.Hash)
	}
	if head.TenantID != testTenant {
		t.Errorf("genesis TenantID = %q, want %q", head.TenantID, testTenant)
	}
}

// 보조: 검증 에러 (Action 비어있음).
func TestAppendValidationErrors(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	cases := []struct {
		name    string
		mutate  func(r *audit.AppendRequest)
		wantErr error
	}{
		{"empty action", func(r *audit.AppendRequest) { r.Action = "" }, audit.ErrEmptyAction},
		{"empty target type", func(r *audit.AppendRequest) { r.Target.Type = "" }, audit.ErrEmptyTarget},
		{"empty target id", func(r *audit.AppendRequest) { r.Target.ID = "" }, audit.ErrEmptyTarget},
		{"invalid actor", func(r *audit.AppendRequest) { r.Actor.Type = "alien" }, audit.ErrInvalidActor},
		{"invalid outcome", func(r *audit.AppendRequest) { r.Outcome = "weird" }, audit.ErrInvalidOutcome},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := sampleReq("robot.create")
			tc.mutate(&req)
			ctx := storage.WithTenantID(context.Background(), testTenant)
			err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
				_, e := repo.Append(ctx, tx, req)
				return e
			})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// E2.T6
func TestVerifyAcceptsCleanRange(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	for i := 0; i < 5; i++ {
		appendOne(t, store, repo, sampleReq("robot.create"))
	}

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var result audit.VerifyResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Verify(ctx, tx, testTenant, 1, 5)
		result = r
		return err
	}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.OK {
		t.Errorf("OK = false, want true; BreakAt=%d Reason=%q", result.BreakAt, result.Reason)
	}
	if result.EntriesScanned != 5 {
		t.Errorf("EntriesScanned = %d, want 5", result.EntriesScanned)
	}
}

// E2.T6 보조: toSeq 생략 시 head까지 검증.
func TestVerifyDefaultsToHead(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	for i := 0; i < 3; i++ {
		appendOne(t, store, repo, sampleReq("robot.create"))
	}
	ctx := storage.WithTenantID(context.Background(), testTenant)

	var result audit.VerifyResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Verify(ctx, tx, testTenant, 0, 0) // both default
		result = r
		return err
	}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.OK || result.EntriesScanned != 3 {
		t.Errorf("got OK=%v scanned=%d, want OK=true scanned=3", result.OK, result.EntriesScanned)
	}
}

// E2.T6 보조: 빈 체인 → OK=true, scanned=0.
func TestVerifyEmptyChainOK(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var result audit.VerifyResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Verify(ctx, tx, testTenant, 1, 0)
		result = r
		return err
	}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.OK || result.EntriesScanned != 0 {
		t.Errorf("got OK=%v scanned=%d, want OK=true scanned=0", result.OK, result.EntriesScanned)
	}
}

// E2.T5 — hash 위변조 감지.
// 정상 append 1개 후, raw INSERT로 잘못된 hash entry 추가 → Verify가 위치를 정확히 가리킨다.
func TestVerifyDetectsHashTampering(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	first := appendOne(t, store, repo, sampleReq("robot.create"))

	// raw INSERT — hash를 0xFF...로 채워 일부러 깨뜨림.
	ctx := storage.WithTenantID(context.Background(), testTenant)
	tamperedHash := make([]byte, audit.HashSize)
	for i := range tamperedHash {
		tamperedHash[i] = 0xFF
	}
	occurredAt := first.OccurredAt.Add(time.Millisecond).UTC().Format(time.RFC3339Nano)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO audit_entries (
    tenant_id, seq, occurred_at,
    actor_type, actor_id, actor_ip, actor_ua,
    action, target_type, target_id,
    payload_digest, outcome, error_code, error_message,
    prev_hash, hash
) VALUES (?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)`,
			string(testTenant), int64(2), occurredAt,
			"user", "us_attacker",
			"robot.delete", "robot", "ro_test",
			make([]byte, audit.HashSize), "success",
			first.Hash[:], tamperedHash) // 정확한 prev_hash, 깨진 hash
		return err
	}); err != nil {
		t.Fatalf("raw INSERT: %v", err)
	}

	// Verify가 seq=2에서 위반 감지해야 함.
	var result audit.VerifyResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Verify(ctx, tx, testTenant, 1, 2)
		result = r
		return err
	}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.OK {
		t.Error("OK = true, want false (tampered hash)")
	}
	if result.BreakAt != 2 {
		t.Errorf("BreakAt = %d, want 2", result.BreakAt)
	}
	if result.Reason == "" {
		t.Error("Reason should describe the failure")
	}
	t.Logf("verify reason: %s", result.Reason)
}

// E2.T5 보조: prev_hash 단절 감지.
// raw INSERT로 prev_hash가 첫 entry.hash와 다른 entry 삽입 → Verify가 chain break 감지.
func TestVerifyDetectsPrevHashBreak(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	first := appendOne(t, store, repo, sampleReq("robot.create"))

	// raw INSERT: prev_hash=zeros (잘못된 값) + hash 임의 값.
	ctx := storage.WithTenantID(context.Background(), testTenant)
	occurredAt := first.OccurredAt.Add(time.Millisecond).UTC().Format(time.RFC3339Nano)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO audit_entries (
    tenant_id, seq, occurred_at,
    actor_type, actor_id, actor_ip, actor_ua,
    action, target_type, target_id,
    payload_digest, outcome, error_code, error_message,
    prev_hash, hash
) VALUES (?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)`,
			string(testTenant), int64(2), occurredAt,
			"user", "us_attacker",
			"robot.update", "robot", "ro_test",
			make([]byte, audit.HashSize), "success",
			make([]byte, audit.HashSize), make([]byte, audit.HashSize)) // 둘 다 zero
		return err
	}); err != nil {
		t.Fatalf("raw INSERT: %v", err)
	}

	var result audit.VerifyResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Verify(ctx, tx, testTenant, 1, 2)
		result = r
		return err
	}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.OK {
		t.Error("OK = true, want false (prev_hash break)")
	}
	if result.BreakAt != 2 {
		t.Errorf("BreakAt = %d, want 2", result.BreakAt)
	}
}

// E2.T5 보조: 누락된 seq 감지 (gap).
func TestVerifyDetectsMissingSeq(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	appendOne(t, store, repo, sampleReq("robot.create")) // seq 1

	// 사용자가 seq=3을 명시적으로 요청 — head가 1이지만 3까지 검증 요청 → 누락.
	ctx := storage.WithTenantID(context.Background(), testTenant)
	var result audit.VerifyResult
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := repo.Verify(ctx, tx, testTenant, 1, 3)
		result = r
		return err
	}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.OK {
		t.Error("OK = true, want false (missing seqs)")
	}
	if result.BreakAt != 2 {
		t.Errorf("BreakAt = %d, want 2", result.BreakAt)
	}
}

// E2.T7
func TestExportNDJSONIncludesSignature(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	for i := 0; i < 3; i++ {
		appendOne(t, store, repo, sampleReq("robot.create"))
	}

	sgn, err := soft.New()
	if err != nil {
		t.Fatalf("soft.New: %v", err)
	}

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var lines []string
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		rc, err := repo.Export(ctx, tx, testTenant, 1, 3, sgn)
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()

		gz, err := gzip.NewReader(rc)
		if err != nil {
			return err
		}
		defer func() { _ = gz.Close() }()

		scanner := bufio.NewScanner(gz)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		return scanner.Err()
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4 (3 entries + 1 signature)", len(lines))
	}

	// 마지막 라인은 signature.
	var sig audit.ExportSignatureLine
	if err := json.Unmarshal([]byte(lines[3]), &sig); err != nil {
		t.Fatalf("decode signature line: %v", err)
	}
	if sig.From != 1 || sig.To != 3 {
		t.Errorf("range = %d~%d, want 1~3", sig.From, sig.To)
	}
	if sig.KeyID != sgn.KeyID() {
		t.Errorf("keyID = %q, want %q", sig.KeyID, sgn.KeyID())
	}
	if sig.PublicKey != hex.EncodeToString(sgn.PublicKey()) {
		t.Errorf("publicKey hex mismatch")
	}
	if sig.Signature == "" {
		t.Error("signature empty")
	}

	// 외부 검증 도구 시뮬레이션:
	// 1. entry 라인들을 다시 buffer에 합쳐 sha256
	// 2. signer.Verify(payload, signature) 통과
	var entryBuf strings.Builder
	for i := 0; i < 3; i++ {
		entryBuf.WriteString(lines[i])
		entryBuf.WriteByte('\n')
	}
	digest := sha256.Sum256([]byte(entryBuf.String()))
	if hex.EncodeToString(digest[:]) != sig.SignedDigest {
		t.Errorf("recomputed digest != signed digest\n got  %s\n want %s",
			hex.EncodeToString(digest[:]), sig.SignedDigest)
	}

	sigBytes, err := hex.DecodeString(sig.Signature)
	if err != nil {
		t.Fatalf("decode signature hex: %v", err)
	}
	if err := sgn.Verify(digest[:], sigBytes); err != nil {
		t.Errorf("signer.Verify failed: %v", err)
	}
}

// E2.T7 보조: 빈 체인 → entries 0개, signature 라인만 남음.
func TestExportEmptyChainSignsZeroBytes(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	sgn, err := soft.New()
	if err != nil {
		t.Fatalf("soft.New: %v", err)
	}

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var content []byte
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		rc, err := repo.Export(ctx, tx, testTenant, 1, 0, sgn)
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()

		gz, err := gzip.NewReader(rc)
		if err != nil {
			return err
		}
		defer func() { _ = gz.Close() }()

		content, err = io.ReadAll(gz)
		return err
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1 (signature only)", len(lines))
	}
	var sig audit.ExportSignatureLine
	if err := json.Unmarshal([]byte(lines[0]), &sig); err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	emptyDigest := sha256.Sum256(nil)
	if sig.SignedDigest != hex.EncodeToString(emptyDigest[:]) {
		t.Errorf("empty digest mismatch")
	}
}

// E2.T7 보조: signer 누락 → 명시적 에러.
func TestExportRequiresSigner(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	ctx := storage.WithTenantID(context.Background(), testTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.Export(ctx, tx, testTenant, 1, 0, nil)
		return e
	})
	if err == nil {
		t.Error("expected error for nil signer")
	}
}

// Phase 10.D-5 — ExportV2 wire 회귀 가드.
//
// nil keyRepo → v1 wire 와 byte-identical (legacy fallback).
// 비-nil keyRepo → _bundleVersion="v2" + _chainKeyEpochs[] 포함.
func TestExportV2EmitsBundleVersionAndChainKeyEpochs(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)
	keyRepo := sqliterepo.NewKeyEpochRepo()

	// audit_chain_keys 는 'system' tenant 에만 bootstrap 됨 — system tenant 로 테스트.
	const sysTenant storage.TenantID = "system"

	sysReq := audit.AppendRequest{
		TenantID: sysTenant,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "scheduler"},
		Action:   "audit.chain.key_rotated",
		Target:   audit.Target{Type: "chain", ID: "system"},
		Outcome:  audit.OutcomeSuccess,
	}
	appendOne(t, store, repo, sysReq)

	sgn, err := soft.New()
	if err != nil {
		t.Fatalf("soft.New: %v", err)
	}

	ctx := storage.WithTenantID(context.Background(), sysTenant)
	var lines []string
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		rc, err := repo.ExportV2(ctx, tx, sysTenant, 1, 1, sgn, keyRepo)
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		gz, err := gzip.NewReader(rc)
		if err != nil {
			return err
		}
		defer func() { _ = gz.Close() }()
		scanner := bufio.NewScanner(gz)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		return scanner.Err()
	}); err != nil {
		t.Fatalf("ExportV2: %v", err)
	}

	if len(lines) < 2 {
		t.Fatalf("got %d lines, want >= 2 (entry + signature)", len(lines))
	}
	var sig audit.ExportSignatureLine
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &sig); err != nil {
		t.Fatalf("decode signature line: %v", err)
	}
	if sig.BundleVersion != audit.BundleVersionV2 {
		t.Errorf("BundleVersion=%q want %q", sig.BundleVersion, audit.BundleVersionV2)
	}
	if len(sig.ChainKeyEpochs) < 1 {
		t.Errorf("ChainKeyEpochs len=%d want >= 1 (bootstrap)", len(sig.ChainKeyEpochs))
	}
	// bootstrap row 가 첫 epoch 으로 노출되어야 함.
	if sig.ChainKeyEpochs[0].Epoch != 1 {
		t.Errorf("first epoch=%d want 1 (bootstrap)", sig.ChainKeyEpochs[0].Epoch)
	}
}

// nil keyRepo 는 v1 fallback (Export 와 동일 wire).
func TestExportV2NilKeyRepoFallsBackToV1(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	appendOne(t, store, repo, sampleReq("robot.create"))
	sgn, err := soft.New()
	if err != nil {
		t.Fatalf("soft.New: %v", err)
	}

	ctx := storage.WithTenantID(context.Background(), testTenant)
	var lines []string
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		rc, err := repo.ExportV2(ctx, tx, testTenant, 1, 1, sgn, nil)
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		gz, err := gzip.NewReader(rc)
		if err != nil {
			return err
		}
		defer func() { _ = gz.Close() }()
		scanner := bufio.NewScanner(gz)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		return scanner.Err()
	}); err != nil {
		t.Fatalf("ExportV2 nil keyRepo: %v", err)
	}
	var sig audit.ExportSignatureLine
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &sig); err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	if sig.BundleVersion != "" {
		t.Errorf("BundleVersion=%q want empty (v1 fallback)", sig.BundleVersion)
	}
	if len(sig.ChainKeyEpochs) != 0 {
		t.Errorf("ChainKeyEpochs len=%d want 0 (v1 fallback)", len(sig.ChainKeyEpochs))
	}
}

// E2.T8 — checkpoint 서명·저장.
func TestWriteCheckpointStoresVerifiableSignature(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	for i := 0; i < 3; i++ {
		appendOne(t, store, repo, sampleReq("robot.create"))
	}
	sgn, err := soft.New()
	if err != nil {
		t.Fatalf("soft.New: %v", err)
	}

	ctx := storage.WithTenantID(context.Background(), testTenant)

	var written audit.Checkpoint
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		cp, err := repo.WriteCheckpoint(ctx, tx, testTenant, sgn)
		written = cp
		return err
	}); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	if written.Seq != 3 {
		t.Errorf("Seq = %d, want 3 (= head)", written.Seq)
	}
	if written.SignerKeyID != sgn.KeyID() {
		t.Errorf("SignerKeyID = %q, want %q", written.SignerKeyID, sgn.KeyID())
	}

	// payload를 동일하게 재구성하여 signer.Verify 통과.
	payload := audit.SerializeCheckpointPayload(written.TenantID, written.Seq, written.Hash)
	if err := sgn.Verify(payload, written.Signature); err != nil {
		t.Errorf("signer.Verify failed: %v", err)
	}

	// LatestCheckpoint도 같은 row 반환.
	var latest audit.Checkpoint
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		c, e := repo.LatestCheckpoint(ctx, tx, testTenant)
		latest = c
		return e
	}); err != nil {
		t.Fatalf("LatestCheckpoint: %v", err)
	}
	if latest.Seq != written.Seq || latest.Hash != written.Hash || latest.SignerKeyID != written.SignerKeyID {
		t.Errorf("LatestCheckpoint mismatch:\n got  %+v\n want %+v", latest, written)
	}
}

// E2.T8 보조: 빈 체인 → ErrNoEntries.
func TestWriteCheckpointEmptyChainNoEntries(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)
	sgn, _ := soft.New()

	ctx := storage.WithTenantID(context.Background(), testTenant)
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.WriteCheckpoint(ctx, tx, testTenant, sgn)
		return e
	})
	if !errors.Is(err, audit.ErrNoEntries) {
		t.Errorf("err = %v, want ErrNoEntries", err)
	}
}

// E2.T8 보조: 같은 head로 두 번 → ErrCheckpointExists.
func TestWriteCheckpointDuplicateAtSameHead(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)
	appendOne(t, store, repo, sampleReq("robot.create"))
	sgn, _ := soft.New()

	ctx := storage.WithTenantID(context.Background(), testTenant)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.WriteCheckpoint(ctx, tx, testTenant, sgn)
		return e
	}); err != nil {
		t.Fatalf("first checkpoint: %v", err)
	}

	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.WriteCheckpoint(ctx, tx, testTenant, sgn)
		return e
	})
	if !errors.Is(err, audit.ErrCheckpointExists) {
		t.Errorf("second checkpoint: err = %v, want ErrCheckpointExists", err)
	}
}

// E2.T8 보조: checkpoint 테이블도 immutable trigger 보호.
func TestWriteCheckpointIsImmutable(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)
	appendOne(t, store, repo, sampleReq("robot.create"))
	sgn, _ := soft.New()

	ctx := storage.WithTenantID(context.Background(), testTenant)
	if err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.WriteCheckpoint(ctx, tx, testTenant, sgn)
		return e
	}); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE audit_checkpoints SET signature = X'00' WHERE tenant_id = ?`, string(testTenant))
		return e
	})
	if !errors.Is(err, storage.ErrImmutable) {
		t.Errorf("UPDATE checkpoints: err = %v, want ErrImmutable", err)
	}

	err = store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM audit_checkpoints WHERE tenant_id = ?`, string(testTenant))
		return e
	})
	if !errors.Is(err, storage.ErrImmutable) {
		t.Errorf("DELETE checkpoints: err = %v, want ErrImmutable", err)
	}
}

// 보조: tx.TenantID()와 req.TenantID 불일치 → ErrTenantMismatch.
func TestAppendTenantMismatch(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	ctx := storage.WithTenantID(context.Background(), "tn_a")
	err := store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		req := sampleReq("robot.create")
		req.TenantID = "tn_b" // tx는 tn_a, req는 tn_b
		_, e := repo.Append(ctx, tx, req)
		return e
	})
	if !errors.Is(err, audit.ErrTenantMismatch) {
		t.Errorf("err = %v, want ErrTenantMismatch", err)
	}
}
