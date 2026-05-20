package rotation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Segment는 rotation 단위 메타데이터 + raw entry stream입니다.
//
// Entries는 NDJSON 직렬화 후 Archiver가 tar.gz로 wrap.
// Hash는 entry 들의 hash 를 sequential fold한 sha256 (자세한 fold 정의는 ComputeSegmentHash).
//
// PrevHash는 직전 segment(segment_number-1)의 Hash. 첫 segment(segment_number=1)는 nil — segment
// 간 chain link layer (entry-level prev_hash와 별도). 외부 감사인은 archive manifest 들에 포함된
// prevSegmentHash 필드와 비교해 cold 영역 chain 무결성을 검증합니다.
type Segment struct {
	TenantID     storage.TenantID
	FirstEntryID int64
	LastEntryID  int64
	EntryCount   int64
	StartedAt    time.Time // 첫 entry occurred_at
	EndedAt      time.Time // 마지막 entry occurred_at
	Hash         audit.Hash
	PrevHash     []byte // 직전 segment의 segment_hash (첫 segment는 nil). len 0 또는 32.
	Entries      []audit.Entry
}

// ComputeSegmentHash는 segment 내 entry 들의 hash를 sequential fold합니다.
//
//	segment_hash = sha256( hash_1[32] ‖ hash_2[32] ‖ ... ‖ hash_N[32] )
//
// 외부 검증 도구는 동일 함수로 재현 가능 — 입력은 archive NDJSON의 hash 필드 순서.
// 빈 슬라이스 입력은 sha256("") (모든 input 비트 0개).
func ComputeSegmentHash(entries []audit.Entry) audit.Hash {
	h := sha256.New()
	for _, e := range entries {
		h.Write(e.Hash[:])
	}
	var out audit.Hash
	copy(out[:], h.Sum(nil))
	return out
}

// BuildSegment는 [fromSeq, toSeq] 범위 entry 들을 audit_entries에서 SELECT하여
// Segment 메타데이터 + raw entry 들을 구성합니다.
//
// fromSeq <= 0 또는 toSeq < fromSeq면 error.
// 범위 내 entry가 1개도 없으면 error (rotation 의미 없음).
//
// 본 함수는 tx scope tenant와 인자 tenantID 일치를 강제합니다.
//
// PrevHash는 호출자가 별도로 설정합니다 (BuildSegmentWithPrev 또는 Rotator.Rotate가
// LatestSegmentHash 조회 후 채움). 본 함수는 PrevHash=nil 인 Segment를 반환합니다 — 호환 유지.
func BuildSegment(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fromSeq, toSeq int64) (*Segment, error) {
	return BuildSegmentWithPrev(ctx, tx, tenantID, fromSeq, toSeq, nil)
}

// BuildSegmentWithPrev는 BuildSegment와 동일하되 prevSegmentHash를 함께 설정합니다.
//
// prevSegmentHash가 nil이 아니면 길이 32(audit.HashSize)여야 합니다 — 그 외 길이는 error.
// 본 함수는 단지 메타에 PrevHash를 set 할 뿐 hash 재계산하지 않습니다.
// Segment.Hash는 segment 내 entry 들로만 결정되므로 PrevHash 변경이 Hash에 영향을 주지 않습니다.
func BuildSegmentWithPrev(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fromSeq, toSeq int64, prevSegmentHash []byte) (*Segment, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("rotation: BuildSegment: tenantID required")
	}
	if tx.TenantID() != "" && tx.TenantID() != tenantID {
		return nil, fmt.Errorf("rotation: BuildSegment: tx tenant mismatch (tx=%q, arg=%q)",
			tx.TenantID(), tenantID)
	}
	if fromSeq <= 0 {
		return nil, fmt.Errorf("rotation: BuildSegment: fromSeq must be > 0, got %d", fromSeq)
	}
	if toSeq < fromSeq {
		return nil, fmt.Errorf("rotation: BuildSegment: toSeq (%d) < fromSeq (%d)", toSeq, fromSeq)
	}

	rows, err := tx.Query(ctx, `
SELECT seq, occurred_at, actor_type, actor_id, actor_ip, actor_ua,
       action, target_type, target_id,
       payload_digest, outcome, error_code, error_message,
       prev_hash, hash, leader_epoch
  FROM audit_entries
 WHERE tenant_id = ? AND seq BETWEEN ? AND ?
 ORDER BY seq ASC`,
		string(tenantID), fromSeq, toSeq)
	if err != nil {
		return nil, fmt.Errorf("rotation: BuildSegment query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []audit.Entry
	for rows.Next() {
		e, err := scanRotationEntry(rows, tenantID)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rotation: BuildSegment rows: %w", err)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("rotation: BuildSegment: no entries in range [%d, %d]", fromSeq, toSeq)
	}

	if prevSegmentHash != nil && len(prevSegmentHash) != audit.HashSize {
		return nil, fmt.Errorf("rotation: BuildSegment: prevSegmentHash size = %d, want %d (or nil)",
			len(prevSegmentHash), audit.HashSize)
	}

	// defensive copy — caller가 buffer 재사용해도 segment가 안전.
	var prevCopy []byte
	if len(prevSegmentHash) == audit.HashSize {
		prevCopy = make([]byte, audit.HashSize)
		copy(prevCopy, prevSegmentHash)
	}

	return &Segment{
		TenantID:     tenantID,
		FirstEntryID: entries[0].Seq,
		LastEntryID:  entries[len(entries)-1].Seq,
		EntryCount:   int64(len(entries)),
		StartedAt:    entries[0].OccurredAt,
		EndedAt:      entries[len(entries)-1].OccurredAt,
		Hash:         ComputeSegmentHash(entries),
		PrevHash:     prevCopy,
		Entries:      entries,
	}, nil
}

// scanRotationEntry는 audit_entries row를 audit.Entry로 디코드합니다.
//
// sqliterepo.scanEntry와 동일 스키마. 본 패키지는 sqliterepo를 import할 수 없으므로
// (반대 방향 의존이 안전) 동등 로직을 복사 — schema 변경 시 두 곳을 동기화.
func scanRotationEntry(rows interface {
	Scan(dest ...any) error
}, tenantID storage.TenantID) (audit.Entry, error) {
	var (
		seq                  int64
		occurredStr          string
		actorType, actorID   string
		actorIP, actorUA     sql.NullString
		action               string
		targetType, targetID string
		payloadDigest        []byte
		outcome              string
		errCode, errMessage  sql.NullString
		prevHash, hash       []byte
		leaderEpoch          sql.NullInt64
	)
	if err := rows.Scan(&seq, &occurredStr,
		&actorType, &actorID, &actorIP, &actorUA,
		&action, &targetType, &targetID,
		&payloadDigest, &outcome, &errCode, &errMessage,
		&prevHash, &hash, &leaderEpoch); err != nil {
		return audit.Entry{}, fmt.Errorf("rotation: scan entry: %w", err)
	}

	occurredAt, err := time.Parse(time.RFC3339Nano, occurredStr)
	if err != nil {
		return audit.Entry{}, fmt.Errorf("rotation: parse occurred_at seq=%d: %w", seq, err)
	}

	e := audit.Entry{
		TenantID:   tenantID,
		Seq:        seq,
		OccurredAt: occurredAt,
		Actor: audit.Actor{
			Type:      audit.ActorType(actorType),
			ID:        actorID,
			IP:        actorIP.String,
			UserAgent: actorUA.String,
		},
		Action:  action,
		Target:  audit.Target{Type: targetType, ID: targetID},
		Outcome: audit.Outcome(outcome),
	}
	if errCode.Valid || errMessage.Valid {
		e.Error = &audit.ErrorInfo{Code: errCode.String, Message: errMessage.String}
	}
	if len(payloadDigest) != audit.HashSize {
		return audit.Entry{}, fmt.Errorf("rotation: payload_digest size = %d, want %d (seq=%d)",
			len(payloadDigest), audit.HashSize, seq)
	}
	if len(prevHash) != audit.HashSize {
		return audit.Entry{}, fmt.Errorf("rotation: prev_hash size = %d, want %d (seq=%d)",
			len(prevHash), audit.HashSize, seq)
	}
	if len(hash) != audit.HashSize {
		return audit.Entry{}, fmt.Errorf("rotation: hash size = %d, want %d (seq=%d)",
			len(hash), audit.HashSize, seq)
	}
	copy(e.PayloadDigest[:], payloadDigest)
	copy(e.PrevHash[:], prevHash)
	copy(e.Hash[:], hash)
	if leaderEpoch.Valid {
		ep := leaderEpoch.Int64
		e.LeaderEpoch = &ep
	}
	return e, nil
}
