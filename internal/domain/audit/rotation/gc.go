package rotation

// gc.go — E32 Stage 4: audit chain hot GC.
//
// design: docs/design/notes/audit-chain-rotation-design.md Stage 4.
//
// HotGC는 archive 가 완료된 segment 중 hot retention 기간이 지난 segment 의 audit_entries
// 를 DELETE 합니다. P9 불변성 트리거 (audit_entries_block_delete) 는 session-local GUC
// `rosshield.audit_gc_mode = 'on'` 일 때만 우회됩니다 (마이그레이션 0034 — PG only).
//
// 결정 — 옵션 A (GUC) 채택 이유:
//   - 트리거 자체는 그대로 유지 → application code의 우발적 DELETE 차단 유지.
//   - SET LOCAL은 tx 끝에서 자동 reset → 다른 connection 영향 0.
//   - 함수 시그니처 변경 없음 (DROP/RECREATE 회피).
//   - audit chain immutability 외부 감사 표면 유지 (트리거 존재 자체는 같음).
//
// sqlite 미지원 — sqlite 배포(데스크톱·단일 노드)는 hot row 무한 보존 + cold archive 만
// 생성. PG 멀티 인스턴스 / 어플라이언스 환경 에서만 HotGC 활성 (carryover).
//
// 흐름:
//
//  1. Run(ctx, tx, tenantID, dryRun)
//  2. ListSegmentsArchivedBefore(now - HotRetention) → 후보 segment 들
//  3. dryRun=true 면 entry 추정 카운트만 리턴 (DELETE 미실행, audit.gc.complete emit 안 함)
//  4. dryRun=false 면:
//     a. SET LOCAL rosshield.audit_gc_mode = 'on'
//     b. 각 segment 범위 DELETE FROM audit_entries WHERE tenant_id=? AND seq BETWEEN first_entry_id AND last_entry_id
//     c. oldest kept entry seq 조회 (남은 entries 중 MIN(seq))
//     d. audit.gc.complete entry append (chain link, P9 INSERT 영향 없음)
//
// 본 round 단순화:
//   - segment metadata 자체는 DELETE 안 함 (audit_rotation_segments 도 P9 불변).
//   - cosign bundle / signature 검증은 별 layer (verify CLI).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// ActionGCComplete는 hot GC 완료를 audit chain에 link하는 entry action 이름입니다.
const ActionGCComplete = "audit.gc.complete"

// HotGCResult는 HotGC.Run의 출력입니다.
//
// DryRun=true면 DeletedCount는 추정값 (segment.EntryCount 합산),
// DryRun=false면 실제 DELETE row 수 합산. OldestKeptEntrySeq는 dryRun=true 시 0.
type HotGCResult struct {
	DeletedCount       int64
	SegmentsProcessed  []int64 // segment_number 목록
	OldestKeptEntrySeq int64   // dryRun=false 일 때만; 남은 entries 없으면 0
	DryRun             bool
}

// HotGC는 hot retention 만료 segment의 audit_entries를 DELETE합니다.
//
// 의존:
//   - policy: HotRetention만 사용 (다른 필드는 cron / scheduler 책임).
//   - appender: audit.gc.complete entry emit (nil 허용 — emit skip, 진단 용도).
//   - clock: now 추출.
type HotGC struct {
	policy   RotationPolicy
	appender AuditAppender
	clock    clock.Clock
}

// HotGCDeps는 HotGC 의존성입니다.
type HotGCDeps struct {
	Policy   RotationPolicy
	Appender AuditAppender // nil 허용 — emit skip
	Clock    clock.Clock
}

// NewHotGC는 HotGC를 만듭니다.
//
// Policy.Validate() 호출 안 함 — 호출자(scheduler)가 미리 검증한 정책 전달 가정.
// Clock 미주입 시 error.
func NewHotGC(deps HotGCDeps) (*HotGC, error) {
	if deps.Clock == nil {
		return nil, errors.New("rotation: NewHotGC: Clock required")
	}
	return &HotGC{
		policy:   deps.Policy,
		appender: deps.Appender,
		clock:    deps.Clock,
	}, nil
}

// Run은 hot retention 만료된 archived segment의 entries를 DELETE합니다.
//
// dryRun=true면 DELETE 미실행, 추정 count만 리턴 (audit.gc.complete entry emit 안 함).
//
// 진입 가정:
//   - tx는 tenant scope (tx.TenantID() == tenantID) 또는 빈 tenant. 빈 경우는 cron이
//     Storage.Tx 진입 시 tenant 주입을 잊은 상태로, error 반환.
//   - PG 에서 호출 (sqlite는 SET LOCAL 미지원 — 호출은 가능하나 트리거 그대로면 DELETE 거부).
//
// HotRetention == 0 이면 cutoff = now → 모든 segment 대상 (테스트 편의). 양수 권장.
//
// 단일 Tx 안에서 SET LOCAL이 모든 DELETE 에 적용됩니다 (SET LOCAL은 tx scope).
func (g *HotGC) Run(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, dryRun bool) (*HotGCResult, error) {
	if tenantID == "" {
		return nil, errors.New("rotation: HotGC.Run: tenantID required")
	}
	if tx.TenantID() != "" && tx.TenantID() != tenantID {
		return nil, fmt.Errorf("rotation: HotGC.Run: tx tenant mismatch (tx=%q, arg=%q)",
			tx.TenantID(), tenantID)
	}

	now := g.clock.Now().UTC()
	cutoff := now.Add(-g.policy.HotRetention)

	segments, err := ListSegmentsArchivedBefore(ctx, tx, tenantID, cutoff)
	if err != nil {
		return nil, err
	}

	result := &HotGCResult{DryRun: dryRun}

	if len(segments) == 0 {
		return result, nil
	}

	if dryRun {
		for _, seg := range segments {
			result.DeletedCount += seg.EntryCount
			result.SegmentsProcessed = append(result.SegmentsProcessed, seg.SegmentNumber)
		}
		return result, nil
	}

	// session-local GC mode 활성화 — tx 끝에서 자동 reset.
	// PG 만 — sqlite 는 트리거가 그대로 RAISE(ABORT) 차단.
	if _, err := tx.Exec(ctx, "SET LOCAL rosshield.audit_gc_mode = 'on'"); err != nil {
		return nil, fmt.Errorf("rotation: HotGC.Run: SET LOCAL: %w", err)
	}

	for _, seg := range segments {
		res, err := tx.Exec(ctx,
			`DELETE FROM audit_entries WHERE tenant_id = ? AND seq BETWEEN ? AND ?`,
			string(tenantID), seg.FirstEntryID, seg.LastEntryID)
		if err != nil {
			return nil, fmt.Errorf("rotation: HotGC.Run: DELETE seg=%d: %w", seg.SegmentNumber, err)
		}
		rowsAffected, raErr := res.RowsAffected()
		if raErr != nil {
			// 일부 드라이버 미지원 — 추정값 fallback (segment EntryCount).
			rowsAffected = seg.EntryCount
		}
		result.DeletedCount += rowsAffected
		result.SegmentsProcessed = append(result.SegmentsProcessed, seg.SegmentNumber)
	}

	// oldest kept entry seq 조회 (남은 entries 중 MIN(seq)) — audit payload 메타 용도.
	row := tx.QueryRow(ctx,
		`SELECT COALESCE(MIN(seq), 0) FROM audit_entries WHERE tenant_id = ?`,
		string(tenantID))
	if err := row.Scan(&result.OldestKeptEntrySeq); err != nil {
		return nil, fmt.Errorf("rotation: HotGC.Run: MIN(seq) scan: %w", err)
	}

	// audit.gc.complete entry emit (chain link). appender nil 이면 skip.
	if g.appender != nil {
		payload, perr := buildGCPayload(result, cutoff)
		if perr != nil {
			return nil, perr
		}
		_, err := g.appender.Append(ctx, tx, audit.AppendRequest{
			TenantID: tenantID,
			Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
			Action:   ActionGCComplete,
			Target:   audit.Target{Type: "audit_chain", ID: string(tenantID)},
			Payload:  payload,
			Outcome:  audit.OutcomeSuccess,
		})
		if err != nil {
			return nil, fmt.Errorf("rotation: HotGC.Run: emit gc.complete: %w", err)
		}
	}

	return result, nil
}

// ListSegmentsArchivedBefore는 archive_uri NOT NULL 이고 created_at < before 인
// segment 들을 segment_number ASC 로 반환합니다.
//
// "archive 완료" 정의: archive_uri NOT NULL (Rotator가 backend.Put 성공 후에만 INSERT).
// archive_sha256 도 NOT NULL 이지만 archive_uri 검사로 충분 (둘 다 같은 시점에 채움).
//
// before 는 cutoff 시각 (now - HotRetention). PG/sqlite 모두 TEXT(RFC3339Nano) 비교.
func ListSegmentsArchivedBefore(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, before time.Time) ([]SegmentRecord, error) {
	if tenantID == "" {
		return nil, errors.New("rotation: ListSegmentsArchivedBefore: tenantID required")
	}
	rows, err := tx.Query(ctx, `
SELECT id, segment_number, started_at, ended_at, first_entry_id, last_entry_id, entry_count,
       segment_hash, prev_segment_hash, archive_uri, archive_sha256, cosign_bundle, created_at
  FROM audit_rotation_segments
 WHERE tenant_id = ? AND archive_uri IS NOT NULL AND created_at < ?
 ORDER BY segment_number ASC`,
		string(tenantID), before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("rotation: ListSegmentsArchivedBefore query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SegmentRecord
	for rows.Next() {
		rec, err := scanSegmentRow(rows, tenantID)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rotation: ListSegmentsArchivedBefore rows: %w", err)
	}
	return out, nil
}

// scanSegmentRow는 audit_rotation_segments 한 row를 SegmentRecord로 디코드합니다.
//
// GetSegment의 scan 로직과 동등 — 두 곳을 schema 변경 시 동기화.
func scanSegmentRow(rows interface {
	Scan(dest ...any) error
}, tenantID storage.TenantID) (SegmentRecord, error) {
	var (
		id            int64
		segNum        int64
		startedStr    string
		endedStr      string
		firstID       int64
		lastID        int64
		count         int64
		segHash       []byte
		prevSegHash   []byte
		archiveURI    *string
		archiveSHA256 []byte
		cosignBundle  []byte
		createdStr    string
	)
	if err := rows.Scan(&id, &segNum, &startedStr, &endedStr, &firstID, &lastID, &count,
		&segHash, &prevSegHash, &archiveURI, &archiveSHA256, &cosignBundle, &createdStr); err != nil {
		return SegmentRecord{}, fmt.Errorf("rotation: scanSegmentRow: %w", err)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, startedStr)
	if err != nil {
		return SegmentRecord{}, fmt.Errorf("rotation: parse started_at: %w", err)
	}
	endedAt, err := time.Parse(time.RFC3339Nano, endedStr)
	if err != nil {
		return SegmentRecord{}, fmt.Errorf("rotation: parse ended_at: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdStr)
	if err != nil {
		return SegmentRecord{}, fmt.Errorf("rotation: parse created_at: %w", err)
	}
	if len(segHash) != audit.HashSize {
		return SegmentRecord{}, fmt.Errorf("rotation: segment_hash size = %d, want %d", len(segHash), audit.HashSize)
	}
	rec := SegmentRecord{
		ID:            id,
		TenantID:      tenantID,
		SegmentNumber: segNum,
		StartedAt:     startedAt,
		EndedAt:       endedAt,
		FirstEntryID:  firstID,
		LastEntryID:   lastID,
		EntryCount:    count,
		ArchiveSHA256: archiveSHA256,
		CosignBundle:  cosignBundle,
		CreatedAt:     createdAt,
	}
	if archiveURI != nil {
		rec.ArchiveURI = *archiveURI
	}
	copy(rec.SegmentHash[:], segHash)
	if len(prevSegHash) == audit.HashSize {
		rec.PrevSegmentHash = make([]byte, audit.HashSize)
		copy(rec.PrevSegmentHash, prevSegHash)
	}
	return rec, nil
}

// buildGCPayload는 audit.gc.complete entry의 canonical JSON payload를 만듭니다.
//
// 형식 (key alphabet 순):
//
//	{
//	  "cutoffAt":           "RFC3339Nano",
//	  "deletedCount":       <N>,
//	  "oldestKeptEntrySeq": <i>,
//	  "segmentNumbers":     [<n1>, <n2>, ...]
//	}
func buildGCPayload(result *HotGCResult, cutoff time.Time) ([]byte, error) {
	type payload struct {
		CutoffAt           string  `json:"cutoffAt"`
		DeletedCount       int64   `json:"deletedCount"`
		OldestKeptEntrySeq int64   `json:"oldestKeptEntrySeq"`
		SegmentNumbers     []int64 `json:"segmentNumbers"`
	}
	p := payload{
		CutoffAt:           cutoff.UTC().Format(time.RFC3339Nano),
		DeletedCount:       result.DeletedCount,
		OldestKeptEntrySeq: result.OldestKeptEntrySeq,
		SegmentNumbers:     result.SegmentsProcessed,
	}
	if p.SegmentNumbers == nil {
		p.SegmentNumbers = []int64{} // canonical JSON [] (null 회피)
	}
	return json.Marshal(p)
}

