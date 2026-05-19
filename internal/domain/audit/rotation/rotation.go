package rotation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// ActionRotateComplete은 rotation 완료를 audit chain에 link하는 entry action 이름입니다.
const ActionRotateComplete = "audit.rotate.complete"

// SegmentRecord는 audit_rotation_segments 한 row의 메타데이터입니다.
type SegmentRecord struct {
	ID            int64
	TenantID      storage.TenantID
	SegmentNumber int64
	StartedAt     time.Time
	EndedAt       time.Time
	FirstEntryID  int64
	LastEntryID   int64
	EntryCount    int64
	SegmentHash   audit.Hash
	ArchiveURI    string
	ArchiveSHA256 []byte
	CosignBundle  []byte
	CreatedAt     time.Time
}

// AuditAppender는 rotation이 의존하는 audit.Service의 최소 표면입니다.
//
// 도메인 경계 (CLAUDE.md): rotation 패키지는 audit.Service 전체가 아니라 Append만 필요.
// 본 interface로 협소하게 표현 — 테스트 시 mock 단순화.
type AuditAppender interface {
	Append(ctx context.Context, tx storage.Tx, req audit.AppendRequest) (audit.Entry, error)
}

// Rotator는 rotation 수행 진입점입니다.
type Rotator struct {
	clock    clock.Clock
	backend  Backend
	appender AuditAppender
}

// Deps는 Rotator 의존성입니다.
type Deps struct {
	Clock    clock.Clock
	Backend  Backend
	Appender AuditAppender
}

// New는 Rotator를 만듭니다.
func New(deps Deps) (*Rotator, error) {
	if deps.Clock == nil {
		return nil, errors.New("rotation: New: Clock required")
	}
	if deps.Backend == nil {
		return nil, errors.New("rotation: New: Backend required")
	}
	if deps.Appender == nil {
		return nil, errors.New("rotation: New: Appender required")
	}
	return &Rotator{
		clock:    deps.Clock,
		backend:  deps.Backend,
		appender: deps.Appender,
	}, nil
}

// Rotate는 [fromSeq, toSeq] 범위 entry 들을 archive + 메타 INSERT + audit.rotate.complete entry emit
// 까지 단일 Tx에 묶어 수행합니다.
//
// 동일 Tx 안에서:
//  1. BuildSegment → in-memory entries + hash.
//  2. Archive → backend.Put (Tx scope 외 I/O — backend 실패 시 Tx rollback 안전).
//  3. INSERT audit_rotation_segments.
//  4. AuditAppender.Append(ActionRotateComplete) — chain link.
//
// segmentNumber는 tenant 내 단조 증가 — 호출자가 (직전 segment + 1) 또는 1 (첫 rotation) 전달.
// 동일 (tenantID, segmentNumber) 중복 INSERT는 UNIQUE 제약 위반.
func (r *Rotator) Rotate(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, segmentNumber, fromSeq, toSeq int64) (*SegmentRecord, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("rotation: Rotate: tenantID required")
	}
	if segmentNumber <= 0 {
		return nil, fmt.Errorf("rotation: Rotate: segmentNumber must be > 0, got %d", segmentNumber)
	}

	segment, err := BuildSegment(ctx, tx, tenantID, fromSeq, toSeq)
	if err != nil {
		return nil, err
	}

	now := r.clock.Now().UTC()
	key := defaultArchiveKey(tenantID, segmentNumber)

	uri, sum, err := Archive(ctx, segment, r.backend, key, now)
	if err != nil {
		return nil, err
	}

	rec := SegmentRecord{
		TenantID:      tenantID,
		SegmentNumber: segmentNumber,
		StartedAt:     segment.StartedAt,
		EndedAt:       segment.EndedAt,
		FirstEntryID:  segment.FirstEntryID,
		LastEntryID:   segment.LastEntryID,
		EntryCount:    segment.EntryCount,
		SegmentHash:   segment.Hash,
		ArchiveURI:    uri,
		ArchiveSHA256: sum,
		CreatedAt:     now,
	}

	id, err := insertSegment(ctx, tx, rec, now)
	if err != nil {
		return nil, err
	}
	rec.ID = id

	if err := r.emitRotateComplete(ctx, tx, rec); err != nil {
		return nil, err
	}

	return &rec, nil
}

// LatestSegmentNumber는 tenant의 가장 최근 segment_number를 반환합니다.
// 없으면 0 (첫 rotation 호출자는 1을 segmentNumber로 사용).
func LatestSegmentNumber(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) (int64, error) {
	row := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(segment_number), 0) FROM audit_rotation_segments WHERE tenant_id = ?`,
		string(tenantID))
	var n int64
	if err := row.Scan(&n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("rotation: latest segment query: %w", err)
	}
	return n, nil
}

// GetSegment는 (tenantID, segmentNumber)로 SegmentRecord를 조회합니다. 없으면 storage.ErrNotFound.
func GetSegment(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, segmentNumber int64) (*SegmentRecord, error) {
	row := tx.QueryRow(ctx, `
SELECT id, started_at, ended_at, first_entry_id, last_entry_id, entry_count,
       segment_hash, archive_uri, archive_sha256, cosign_bundle, created_at
  FROM audit_rotation_segments
 WHERE tenant_id = ? AND segment_number = ?`,
		string(tenantID), segmentNumber)

	var (
		id            int64
		startedStr    string
		endedStr      string
		firstID       int64
		lastID        int64
		count         int64
		segHash       []byte
		archiveURI    sql.NullString
		archiveSHA256 []byte
		cosignBundle  []byte
		createdStr    string
	)
	if err := row.Scan(&id, &startedStr, &endedStr, &firstID, &lastID, &count,
		&segHash, &archiveURI, &archiveSHA256, &cosignBundle, &createdStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, storage.ErrNotFound
		}
		return nil, fmt.Errorf("rotation: GetSegment scan: %w", err)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, startedStr)
	if err != nil {
		return nil, fmt.Errorf("rotation: parse started_at: %w", err)
	}
	endedAt, err := time.Parse(time.RFC3339Nano, endedStr)
	if err != nil {
		return nil, fmt.Errorf("rotation: parse ended_at: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdStr)
	if err != nil {
		return nil, fmt.Errorf("rotation: parse created_at: %w", err)
	}
	if len(segHash) != audit.HashSize {
		return nil, fmt.Errorf("rotation: segment_hash size = %d, want %d", len(segHash), audit.HashSize)
	}
	rec := &SegmentRecord{
		ID:            id,
		TenantID:      tenantID,
		SegmentNumber: segmentNumber,
		StartedAt:     startedAt,
		EndedAt:       endedAt,
		FirstEntryID:  firstID,
		LastEntryID:   lastID,
		EntryCount:    count,
		ArchiveURI:    archiveURI.String,
		ArchiveSHA256: archiveSHA256,
		CosignBundle:  cosignBundle,
		CreatedAt:     createdAt,
	}
	copy(rec.SegmentHash[:], segHash)
	return rec, nil
}

// emitRotateComplete은 audit chain에 ActionRotateComplete entry를 link합니다 (D-AR-10).
//
// payload: canonical JSON { segmentNumber, segmentHash, archiveUri, archiveSha256, ... }
// payload_digest = sha256(payload bytes) — audit.AppendRequest.Payload로 전달하면 자동 계산.
func (r *Rotator) emitRotateComplete(ctx context.Context, tx storage.Tx, rec SegmentRecord) error {
	payload, err := buildRotatePayload(rec)
	if err != nil {
		return err
	}

	_, err = r.appender.Append(ctx, tx, audit.AppendRequest{
		TenantID: rec.TenantID,
		Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
		Action:   ActionRotateComplete,
		Target:   audit.Target{Type: "audit_rotation_segment", ID: fmt.Sprintf("%d", rec.SegmentNumber)},
		Payload:  payload,
		Outcome:  audit.OutcomeSuccess,
	})
	if err != nil {
		return fmt.Errorf("rotation: emit rotate.complete entry: %w", err)
	}
	return nil
}

// buildRotatePayload는 ActionRotateComplete entry의 canonical JSON payload를 만듭니다.
//
// 형식 (key alphabet 순):
//
//	{
//	  "archiveSha256": "<hex>",
//	  "archiveUri":    "file://...",
//	  "entryCount":    <N>,
//	  "firstEntryId":  <i>,
//	  "lastEntryId":   <j>,
//	  "segmentHash":   "<hex>",
//	  "segmentNumber": <n>
//	}
func buildRotatePayload(rec SegmentRecord) ([]byte, error) {
	type payload struct {
		ArchiveSha256 string `json:"archiveSha256"`
		ArchiveURI    string `json:"archiveUri"`
		EntryCount    int64  `json:"entryCount"`
		FirstEntryID  int64  `json:"firstEntryId"`
		LastEntryID   int64  `json:"lastEntryId"`
		SegmentHash   string `json:"segmentHash"`
		SegmentNumber int64  `json:"segmentNumber"`
	}
	p := payload{
		ArchiveSha256: hex.EncodeToString(rec.ArchiveSHA256),
		ArchiveURI:    rec.ArchiveURI,
		EntryCount:    rec.EntryCount,
		FirstEntryID:  rec.FirstEntryID,
		LastEntryID:   rec.LastEntryID,
		SegmentHash:   hex.EncodeToString(rec.SegmentHash[:]),
		SegmentNumber: rec.SegmentNumber,
	}
	return json.Marshal(p)
}

// insertSegment는 audit_rotation_segments에 row를 INSERT하고 ID(BIGSERIAL)를 반환합니다.
//
// SQLite: AUTOINCREMENT — last_insert_rowid().
// PG: BIGSERIAL — RETURNING id (TODO PG driver 분기 시).
func insertSegment(ctx context.Context, tx storage.Tx, rec SegmentRecord, now time.Time) (int64, error) {
	res, err := tx.Exec(ctx, `
INSERT INTO audit_rotation_segments (
    tenant_id, segment_number, started_at, ended_at,
    first_entry_id, last_entry_id, entry_count,
    segment_hash, archive_uri, archive_sha256, cosign_bundle, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(rec.TenantID), rec.SegmentNumber,
		rec.StartedAt.UTC().Format(time.RFC3339Nano),
		rec.EndedAt.UTC().Format(time.RFC3339Nano),
		rec.FirstEntryID, rec.LastEntryID, rec.EntryCount,
		rec.SegmentHash[:],
		nullableString(rec.ArchiveURI),
		nullableBytes(rec.ArchiveSHA256),
		nullableBytes(rec.CosignBundle),
		now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("rotation: insert segment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		// PG driver는 LastInsertId 미지원 — RETURNING id로 대체 필요 (별 epic).
		// 본 round (sqlite 기준) OK.
		return 0, fmt.Errorf("rotation: last insert id: %w", err)
	}
	return id, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// defaultArchiveKey는 backend.Put에 전달하는 default 상대 키입니다.
// 형식: <tenantId>/seg-<segmentNumber-zero-padded-6>.tar.gz
func defaultArchiveKey(tenantID storage.TenantID, segmentNumber int64) string {
	return fmt.Sprintf("%s/seg-%06d.tar.gz", string(tenantID), segmentNumber)
}

// segmentNumberBytes는 segmentNumber를 8B big-endian 으로 직렬화합니다 (내부 보조).
func segmentNumberBytes(n int64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(n))
	return buf[:]
}

// SignedDigest는 segmentNumber + segment_hash + archive_sha256 를 fold한
// 외부 검증용 digest입니다 (cosign sign target 후보; 본 round 미사용).
//
//	digest = sha256( segmentNumberBE[8] ‖ segmentHash[32] ‖ archiveSha256[32] )
func SignedDigest(segmentNumber int64, segmentHash audit.Hash, archiveSHA256 []byte) audit.Hash {
	h := sha256.New()
	h.Write(segmentNumberBytes(segmentNumber))
	h.Write(segmentHash[:])
	h.Write(archiveSHA256)
	var out audit.Hash
	copy(out[:], h.Sum(nil))
	return out
}
