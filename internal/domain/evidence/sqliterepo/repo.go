// Package sqliterepo는 evidence.Service의 SQLite 어댑터입니다 (E7 Stage C).
//
// 책임:
//
//	Store          → redact + blobstore.Put + INSERT/SELECT evidence_records + audit emit
//	Read           → SELECT evidence_records + blobstore.Get (hash 검증)
//	LinkToResult   → INSERT evidence_refs (idempotent — UNIQUE 위반은 silently skip)
//	ListForResult  → JOIN evidence_refs · evidence_records (position ASC)
//
// dedup: (tenant_id, sha256) UNIQUE 인덱스 — 같은 평문 두 번 Store 시 두 번째는 dedup.
// blobstore에는 sha 단위 dedup이 별도로 있지만, DB row는 (tenant, sha) 단위로 1행 강제(R9-8).
//
// 평문은 본 어댑터 안에서만 흐름 — Store가 redact 후 blobstore.Put까지 책임지고,
// 호출자에게 평문이 더 이상 노출되지 않습니다(R9-6).
package sqliterepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/evidence"
	"github.com/ssabro/rosshield/internal/platform/blobstore"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

const rfc3339Nano = time.RFC3339Nano

// Deps는 어댑터 의존성입니다.
type Deps struct {
	Clock     clock.Clock
	IDGen     idgen.IDGen
	Audit     evidence.AuditEmitter // bootstrap에서 audit.Service 어댑터 주입
	BlobStore blobstore.Store       // bootstrap에서 fs 어댑터 주입
}

// Repo는 evidence.Service의 SQLite + blobstore 구현입니다.
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// Store는 redact + blobstore.Put + evidence_records INSERT를 일괄 처리합니다.
//
// dedup 흐름:
//  1. raw → Redact → redacted bytes + marks
//  2. sha256(redacted)
//  3. SELECT (tenant_id, sha256) — 있으면 기존 row 반환 (IsNew=false), blobstore.Put 생략
//  4. 없으면 blobstore.Put → INSERT evidence_records → audit emit (IsNew=true)
//
// blobstore.Put은 본질적으로 idempotent(같은 sha면 no-op)이므로 dedup race가 와도 안전.
func (r *Repo) Store(ctx context.Context, tx storage.Tx, in evidence.StoreInput) (evidence.StoreResult, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return evidence.StoreResult{}, storage.ErrTenantMissing
	}
	if in.TenantID != "" && in.TenantID != tenantID {
		return evidence.StoreResult{}, fmt.Errorf("evidence: input tenant=%q != tx tenant=%q", in.TenantID, tenantID)
	}
	if !evidence.ValidContentType(in.ContentType) {
		return evidence.StoreResult{}, fmt.Errorf("%w: %q", evidence.ErrInvalidContentType, in.ContentType)
	}

	redacted, marks := evidence.Redact(in.Raw)
	sum := sha256.Sum256(redacted)
	shaHex := hex.EncodeToString(sum[:])

	// dedup 조회 — 같은 (tenant, sha) 있으면 기존 record 반환.
	if existing, err := r.findBySHA(ctx, tx, tenantID, shaHex); err == nil {
		return evidence.StoreResult{
			EvidenceID: existing.ID,
			SHA256:     existing.SHA256,
			IsNew:      false,
			SizeBytes:  existing.SizeBytes,
			Redactions: existing.Redactions,
		}, nil
	} else if !errors.Is(err, evidence.ErrEvidenceNotFound) {
		return evidence.StoreResult{}, err
	}

	// 신규 — blobstore에 영속.
	if _, err := r.deps.BlobStore.Put(ctx, redacted); err != nil {
		return evidence.StoreResult{}, fmt.Errorf("evidence: blob put: %w", err)
	}

	now := r.deps.Clock.Now().UTC()
	rec := evidence.Record{
		ID:          r.deps.IDGen.New("ev"),
		TenantID:    tenantID,
		SHA256:      shaHex,
		ContentType: in.ContentType,
		SizeBytes:   int64(len(redacted)),
		BlobLocator: "fs:" + shaHex,
		Redactions:  marks,
		CreatedAt:   now,
	}
	redactJSON, err := evidence.MarshalRedactions(marks)
	if err != nil {
		return evidence.StoreResult{}, fmt.Errorf("evidence: marshal redactions: %w", err)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO evidence_records (
    id, tenant_id, sha256, content_type, size_bytes, blob_locator, redactions, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, string(rec.TenantID), rec.SHA256, string(rec.ContentType),
		rec.SizeBytes, rec.BlobLocator, string(redactJSON),
		rec.CreatedAt.Format(rfc3339Nano),
	); err != nil {
		// dedup race — 동시 두 Store가 같은 sha를 INSERT 시도. 한쪽은 UNIQUE 위반.
		// 패자는 다시 SELECT로 기존 record 회수.
		if isUniqueViolation(err) {
			if existing, ferr := r.findBySHA(ctx, tx, tenantID, shaHex); ferr == nil {
				return evidence.StoreResult{
					EvidenceID: existing.ID,
					SHA256:     existing.SHA256,
					IsNew:      false,
					SizeBytes:  existing.SizeBytes,
					Redactions: existing.Redactions,
				}, nil
			}
		}
		return evidence.StoreResult{}, fmt.Errorf("evidence: insert record: %w", err)
	}

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitEvidenceStored(ctx, tx, rec); err != nil {
			return evidence.StoreResult{}, fmt.Errorf("evidence: audit emit: %w", err)
		}
	}

	return evidence.StoreResult{
		EvidenceID: rec.ID,
		SHA256:     rec.SHA256,
		IsNew:      true,
		SizeBytes:  rec.SizeBytes,
		Redactions: rec.Redactions,
	}, nil
}

// Read는 EvidenceID로 메타 + blob bytes를 반환합니다.
func (r *Repo) Read(ctx context.Context, tx storage.Tx, evidenceID string) (evidence.Record, []byte, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return evidence.Record{}, nil, storage.ErrTenantMissing
	}
	if strings.TrimSpace(evidenceID) == "" {
		return evidence.Record{}, nil, evidence.ErrEvidenceNotFound
	}

	rec, err := r.findByID(ctx, tx, tenantID, evidenceID)
	if err != nil {
		return evidence.Record{}, nil, err
	}

	body, err := r.deps.BlobStore.Get(ctx, rec.SHA256)
	if err != nil {
		if errors.Is(err, blobstore.ErrCorrupted) {
			return evidence.Record{}, nil, evidence.ErrBlobCorrupted
		}
		if errors.Is(err, blobstore.ErrNotFound) {
			return evidence.Record{}, nil, fmt.Errorf("evidence: blob missing for %q (sha=%s)", evidenceID, rec.SHA256)
		}
		return evidence.Record{}, nil, fmt.Errorf("evidence: blob get: %w", err)
	}
	return rec, body, nil
}

// LinkToResult는 (scanResultID, evidenceIDs)를 evidence_refs에 INSERT합니다.
//
// 같은 (scan_result_id, evidence_id)는 idempotent — UNIQUE 위반은 silently skip하고
// position은 최초 입력 순서를 보존합니다.
func (r *Repo) LinkToResult(ctx context.Context, tx storage.Tx, scanResultID string, evidenceIDs []string) ([]evidence.RecordedRef, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	if strings.TrimSpace(scanResultID) == "" {
		return nil, evidence.ErrScanResultEmpty
	}
	if len(evidenceIDs) == 0 {
		return nil, evidence.ErrEvidenceIDsEmpty
	}

	now := r.deps.Clock.Now().UTC()
	out := make([]evidence.RecordedRef, 0, len(evidenceIDs))
	for i, evID := range evidenceIDs {
		evID = strings.TrimSpace(evID)
		if evID == "" {
			continue
		}
		ref := evidence.RecordedRef{
			ScanResultID: scanResultID,
			EvidenceID:   evID,
			Position:     i,
			CreatedAt:    now,
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO evidence_refs (scan_result_id, evidence_id, position, created_at)
VALUES (?, ?, ?, ?)`,
			ref.ScanResultID, ref.EvidenceID, ref.Position,
			ref.CreatedAt.Format(rfc3339Nano),
		); err != nil {
			if isUniqueViolation(err) {
				continue // idempotent — 이미 링크되어 있음
			}
			return nil, fmt.Errorf("evidence: insert ref: %w", err)
		}
		out = append(out, ref)
	}
	return out, nil
}

// ListForResult는 한 scan_result에 붙은 모든 evidence 메타를 position ASC로 반환합니다.
//
// JOIN evidence_refs · evidence_records — tenant 격리는 evidence_records.tenant_id 조건.
func (r *Repo) ListForResult(ctx context.Context, tx storage.Tx, scanResultID string) ([]evidence.Record, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	rows, err := tx.Query(ctx, evidenceSelectColumns+`
  FROM evidence_records r
  JOIN evidence_refs f ON f.evidence_id = r.id
 WHERE f.scan_result_id = ? AND r.tenant_id = ?
 ORDER BY f.position ASC, f.created_at ASC`,
		scanResultID, string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("evidence: list for result: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []evidence.Record
	for rows.Next() {
		rec, err := scanEvidenceRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evidence: list iterate: %w", err)
	}
	return out, nil
}

// --- helpers ---

const evidenceSelectColumns = `
SELECT r.id, r.tenant_id, r.sha256, r.content_type,
       r.size_bytes, r.blob_locator, r.redactions, r.created_at`

const evidenceSelectByIDColumns = `
SELECT id, tenant_id, sha256, content_type,
       size_bytes, blob_locator, redactions, created_at`

func (r *Repo) findBySHA(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, sha string) (evidence.Record, error) {
	row := tx.QueryRow(ctx, evidenceSelectByIDColumns+`
  FROM evidence_records
 WHERE tenant_id = ? AND sha256 = ?`,
		string(tenantID), sha)
	rec, err := scanEvidenceRow(row.Scan)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return evidence.Record{}, evidence.ErrEvidenceNotFound
		}
		return evidence.Record{}, err
	}
	return rec, nil
}

func (r *Repo) findByID(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, evID string) (evidence.Record, error) {
	row := tx.QueryRow(ctx, evidenceSelectByIDColumns+`
  FROM evidence_records
 WHERE id = ? AND tenant_id = ?`,
		evID, string(tenantID))
	rec, err := scanEvidenceRow(row.Scan)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return evidence.Record{}, evidence.ErrEvidenceNotFound
		}
		return evidence.Record{}, err
	}
	return rec, nil
}

func scanEvidenceRow(scanFn func(...any) error) (evidence.Record, error) {
	var (
		id, tenantID, sha, contentType, blobLocator, redactionsJSON, createdAt string
		sizeBytes                                                              int64
	)
	if err := scanFn(&id, &tenantID, &sha, &contentType,
		&sizeBytes, &blobLocator, &redactionsJSON, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return evidence.Record{}, storage.ErrNotFound
		}
		return evidence.Record{}, fmt.Errorf("evidence: scan row: %w", err)
	}
	created, err := time.Parse(rfc3339Nano, createdAt)
	if err != nil {
		return evidence.Record{}, fmt.Errorf("evidence: parse created_at: %w", err)
	}
	marks, err := evidence.UnmarshalRedactions([]byte(redactionsJSON))
	if err != nil {
		return evidence.Record{}, fmt.Errorf("evidence: unmarshal redactions: %w", err)
	}
	return evidence.Record{
		ID:          id,
		TenantID:    storage.TenantID(tenantID),
		SHA256:      sha,
		ContentType: evidence.ContentType(contentType),
		SizeBytes:   sizeBytes,
		BlobLocator: blobLocator,
		Redactions:  marks,
		CreatedAt:   created,
	}, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
