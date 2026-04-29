// evidence_integration_test.go — E7 Stage C/D Exit 검증.
//
// scanrun.Orchestrator에 진짜 evidence.Service(+blobstore.fs)를 결선하고 SSH 결과가
// redact·sha256·blob 영속·N:M ref 부착·audit emit 흐름을 모두 통과하는지 종단 검증.
//
// 시나리오: 단일 robot × 단일 check. fakesshd가 stdout에 비밀(password=topsecret)을
// 포함한 응답을 반환 → orchestrator가 evidence.Store(stdout)을 호출 → blob에 redact된
// 형태로 저장.
package scanrun_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/app/scanrun"
	"github.com/ssabro/rosshield/internal/domain/evidence"
	evidencerepo "github.com/ssabro/rosshield/internal/domain/evidence/sqliterepo"
	"github.com/ssabro/rosshield/internal/domain/scan"
	blobfs "github.com/ssabro/rosshield/internal/platform/blobstore/fs"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/sshpool"
	"github.com/ssabro/rosshield/internal/platform/sshpool/sshpooltest"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

func TestIntegrationEvidenceStoreFlow(t *testing.T) {
	verifyNoLeak(t)

	// 1. fakesshd 1개 — stdout에 password=topsecret 포함.
	endpoint := sshpooltest.New(t, func(cmd string) sshpooltest.ExecResponse {
		return sshpooltest.ExecResponse{
			Stdout: "PermitRootLogin no\npassword=topsecret\n",
		}
	})

	// 2. 표준 harness — scanSvc 결선까지 완료.
	h := newHarness(t, 1)
	h.seedFleetAndPack("tn_EV", "fl_EV", "pk_EV")
	h.seedRobots(1)
	h.seedChecks(1)

	// 3. evidence 결선 — blobstore.fs + sqliterepo.
	bs, err := blobfs.New(filepath.Join(t.TempDir(), "blobs"))
	if err != nil {
		t.Fatalf("blobfs.New: %v", err)
	}
	evSvc := evidencerepo.New(evidencerepo.Deps{
		Clock:     clock.System(),
		IDGen:     idgen.NewULID(),
		Audit:     nil, // audit emit은 본 테스트 대상 X (audit chain은 다른 통합이 검증)
		BlobStore: bs,
	})

	// 4. Orchestrator 재구성 — evidence 주입.
	pool := sshpool.New(sshpool.Deps{})
	orch := scanrun.New(scanrun.Deps{
		Scan: h.scanSvc, Storage: h.store,
		Executor: &integrationSSHAdapter{pool: pool}, Evaluator: integrationBenchmarkAdapter{},
		Bus: h.bus, Clock: clock.System(), WorkerLimit: 1,
		Evidence: evSvc,
	})

	checks := []scan.CheckDef{{
		PackCheckID:  "ck_000",
		Code:         "CIS-EV",
		AuditCommand: []string{"sudo", "grep", "-i", "PermitRootLogin", "/etc/ssh/sshd_config"},
		TimeoutSec:   2,
		EvalRuleJSON: []byte(`{"op":"contains","value":"PermitRootLogin no"}`),
	}}
	targets := makeRobotTargetsForEndpoints(h, []*sshpooltest.FakeSSHD{endpoint})

	sessionID := h.startSession(1)
	if err := orch.Run(context.Background(), h.tenantID, sessionID, targets, checks); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 5. evidence_records — 정확히 1행, content_type=stdout, 사이즈는 redact 후 길이.
	type evRow struct {
		ID, SHA256, ContentType, BlobLocator, Redactions string
		SizeBytes                                        int64
	}
	var rows []evRow
	if err := h.store.Tx(storage.WithTenantID(context.Background(), h.tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			r, err := tx.Query(ctx, `SELECT id, sha256, content_type, size_bytes, blob_locator, redactions
FROM evidence_records WHERE tenant_id=? ORDER BY created_at ASC`, string(h.tenantID))
			if err != nil {
				return err
			}
			defer func() { _ = r.Close() }()
			for r.Next() {
				var row evRow
				if err := r.Scan(&row.ID, &row.SHA256, &row.ContentType, &row.SizeBytes, &row.BlobLocator, &row.Redactions); err != nil {
					return err
				}
				rows = append(rows, row)
			}
			return r.Err()
		}); err != nil {
		t.Fatalf("query evidence_records: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("evidence_records rows = %d, want 1 (stdout 1건)", len(rows))
	}
	if rows[0].ContentType != "stdout" {
		t.Fatalf("ContentType=%q, want stdout", rows[0].ContentType)
	}
	if rows[0].BlobLocator != "fs:"+rows[0].SHA256 {
		t.Fatalf("BlobLocator=%q, want fs:<sha>", rows[0].BlobLocator)
	}
	if !strings.Contains(rows[0].Redactions, `"password"`) {
		t.Fatalf("redactions=%q, want password mark", rows[0].Redactions)
	}

	// 6. blob 내용 검증 — redact된 형태(평문 "topsecret" 부재).
	blob, err := bs.Get(context.Background(), rows[0].SHA256)
	if err != nil {
		t.Fatalf("blob Get: %v", err)
	}
	if strings.Contains(string(blob), "topsecret") {
		t.Fatalf("blob에 평문 비밀이 남음: %q", blob)
	}
	if !strings.Contains(string(blob), "[REDACTED:password:") {
		t.Fatalf("blob에 redact 마커 부재: %q", blob)
	}
	// sha256 일관성 — DB의 sha256과 blob 내용 sha256이 일치.
	sum := sha256.Sum256(blob)
	if hex.EncodeToString(sum[:]) != rows[0].SHA256 {
		t.Fatalf("blob sha mismatch: db=%s actual=%x", rows[0].SHA256, sum)
	}

	// 7. evidence_refs — scan_result 1건 ↔ evidence 1건 (position=0).
	type refRow struct {
		ScanResultID, EvidenceID string
		Position                 int
	}
	var refs []refRow
	if err := h.store.Tx(storage.WithTenantID(context.Background(), h.tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			r, err := tx.Query(ctx, `SELECT scan_result_id, evidence_id, position
FROM evidence_refs ORDER BY position ASC`)
			if err != nil {
				return err
			}
			defer func() { _ = r.Close() }()
			for r.Next() {
				var row refRow
				if err := r.Scan(&row.ScanResultID, &row.EvidenceID, &row.Position); err != nil {
					return err
				}
				refs = append(refs, row)
			}
			return r.Err()
		}); err != nil {
		t.Fatalf("query refs: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("evidence_refs rows = %d, want 1", len(refs))
	}
	if refs[0].EvidenceID != rows[0].ID {
		t.Fatalf("ref.EvidenceID=%q, want %q", refs[0].EvidenceID, rows[0].ID)
	}
	if refs[0].Position != 0 {
		t.Fatalf("ref.Position=%d, want 0", refs[0].Position)
	}

	// 8. ListForResult API로 같은 결과를 다시 조회 — domain 표면 검증.
	var listed []evidence.Record
	if err := h.store.Tx(storage.WithTenantID(context.Background(), h.tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			var err error
			listed, err = evSvc.ListForResult(ctx, tx, refs[0].ScanResultID)
			return err
		}); err != nil {
		t.Fatalf("ListForResult: %v", err)
	}
	if len(listed) != 1 || listed[0].SHA256 != rows[0].SHA256 {
		t.Fatalf("ListForResult mismatch: %+v", listed)
	}
	if len(listed[0].Redactions) == 0 {
		t.Fatalf("ListForResult redactions empty: %+v", listed[0])
	}
}

// dedup 시나리오 — 두 robot이 동일 stdout을 반환할 때 evidence_records 1행만 생성.
func TestIntegrationEvidenceDedupAcrossRobots(t *testing.T) {
	verifyNoLeak(t)

	// 두 fakesshd 모두 같은 stdout(decoy 비밀 + 동일 본문) 반환 → sha 동일.
	makeSame := func(_ string) sshpooltest.ExecResponse {
		return sshpooltest.ExecResponse{Stdout: "PermitRootLogin no\nidentical content for dedup\n"}
	}
	endpoints := []*sshpooltest.FakeSSHD{
		sshpooltest.New(t, makeSame),
		sshpooltest.New(t, makeSame),
	}

	h := newHarness(t, 2)
	h.seedFleetAndPack("tn_DD", "fl_DD", "pk_DD")
	h.seedRobots(2)
	h.seedChecks(1)

	bs, err := blobfs.New(filepath.Join(t.TempDir(), "blobs"))
	if err != nil {
		t.Fatalf("blobfs.New: %v", err)
	}
	evSvc := evidencerepo.New(evidencerepo.Deps{
		Clock: clock.System(), IDGen: idgen.NewULID(), BlobStore: bs,
	})

	pool := sshpool.New(sshpool.Deps{})
	orch := scanrun.New(scanrun.Deps{
		Scan: h.scanSvc, Storage: h.store,
		Executor: &integrationSSHAdapter{pool: pool}, Evaluator: integrationBenchmarkAdapter{},
		Bus: h.bus, Clock: clock.System(), WorkerLimit: 2,
		Evidence: evSvc,
	})

	checks := []scan.CheckDef{{
		PackCheckID: "ck_000", Code: "CIS-DD",
		AuditCommand: []string{"sudo", "grep", "-i", "PermitRootLogin", "/etc/ssh/sshd_config"},
		TimeoutSec:   2,
		EvalRuleJSON: []byte(`{"op":"contains","value":"PermitRootLogin no"}`),
	}}
	targets := makeRobotTargetsForEndpoints(h, endpoints)

	sessionID := h.startSession(2)
	if err := orch.Run(context.Background(), h.tenantID, sessionID, targets, checks); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// evidence_records는 같은 sha 1행만 (dedup 히트).
	var count int
	if err := h.store.Tx(storage.WithTenantID(context.Background(), h.tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			return tx.QueryRow(ctx, `SELECT COUNT(*) FROM evidence_records WHERE tenant_id=?`, string(h.tenantID)).Scan(&count)
		}); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("evidence_records count=%d, want 1 (dedup 히트)", count)
	}

	// evidence_refs는 2건 (서로 다른 scan_result 두 개가 같은 evidence를 참조).
	var refCount int
	if err := h.store.Tx(storage.WithTenantID(context.Background(), h.tenantID),
		func(ctx context.Context, tx storage.Tx) error {
			return tx.QueryRow(ctx, `SELECT COUNT(*) FROM evidence_refs`).Scan(&refCount)
		}); err != nil {
		t.Fatalf("refCount: %v", err)
	}
	if refCount != 2 {
		t.Fatalf("evidence_refs count=%d, want 2 (서로 다른 scan_result × 1 evidence)", refCount)
	}
}
