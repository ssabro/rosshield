package evidence

// evidence.go — 도메인 모델 + Service interface (E7 Stage C).
//
// 본 파일은 평문→hash·blob 영속·N:M scan 매핑의 공개 표면을 정의합니다. Redaction
// 엔진은 redaction.go(Stage A), blob 영속은 `internal/platform/blobstore`(Stage B)가
// 책임 — Service 구현체(`sqliterepo`)가 둘을 조합합니다.
//
// 도메인 결합 규칙(P5):
//   - evidence는 다른 도메인 패키지(scan·robot·tenant·audit)를 import하지 않습니다.
//   - audit emit은 `AuditEmitter` 인터페이스로 cmd/* bootstrap이 어댑터 주입.
//   - blobstore는 `internal/platform/blobstore` interface 의존(어댑터 swap 가능).

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// ContentType은 evidence의 원천 종류입니다 (§04.2 contentType enum).
type ContentType string

const (
	ContentStdout         ContentType = "stdout"
	ContentStderr         ContentType = "stderr"
	ContentFile           ContentType = "file"
	ContentConfigSnapshot ContentType = "config-snapshot"
	ContentScreenshot     ContentType = "screenshot"
)

// Record는 한 평문(redact 후)의 dedup 메타입니다.
//
// blob 자체는 BlobLocator가 가리키는 외부 저장소(filesystem)에 저장. ID는 `ev_<ULID>`,
// SHA256은 redact 후 평문 기준(원본 raw 기준 아님 — 다른 redaction 정책으로 같은 비밀이
// 다른 blob이 될 수 있음을 명시).
type Record struct {
	ID          string
	TenantID    storage.TenantID
	SHA256      string // 64자 lowercase hex
	ContentType ContentType
	SizeBytes   int64  // redact 후 평문 길이
	BlobLocator string // "fs:<sha256>" — backend prefix + key (R9-1 fs only Phase 1)
	Redactions  []RedactionMark
	CreatedAt   time.Time
}

// RecordedRef는 LinkToResult 결과 — scan_result 한 건에 붙은 evidence 한 건.
type RecordedRef struct {
	ScanResultID string
	EvidenceID   string
	Position     int
	CreatedAt    time.Time
}

// StoreInput은 Service.Store 입력입니다.
//
// Raw는 redact 적용 *전* 원본 — Service가 redact + blobstore.Put + DB INSERT 일괄 처리.
// 호출자(scanrun Orchestrator)는 SSH 결과를 받자마자 본 함수에 전달 — 평문이 호출자
// 외부로 흘러가지 않게 함(R9-6).
type StoreInput struct {
	TenantID    storage.TenantID
	ContentType ContentType
	Raw         []byte
}

// StoreResult는 Service.Store 반환입니다.
//
// IsNew=true면 evidence_records에 새로 INSERT됨 — false면 dedup 히트(같은 tenant·sha
// 가 이미 있어 기존 ID 반환). 호출자는 IsNew와 무관하게 EvidenceID를 LinkToResult에
// 넘겨 N:M ref 부착.
type StoreResult struct {
	EvidenceID string
	SHA256     string
	IsNew      bool
	SizeBytes  int64
	Redactions []RedactionMark
}

// AuditEmitter는 evidence 도메인 변경을 감사 체인에 기록하는 콜백입니다 (P5 격리).
//
// 본 인터페이스는 cmd/* bootstrap이 audit.Service 어댑터로 주입 — evidence 패키지
// 자체는 audit 패키지를 import하지 않습니다.
//
// 호출 시점: Store가 새 record(IsNew=true)를 INSERT한 직후, 같은 Tx 안에서 호출.
// dedup 히트는 emit하지 않음 — 이미 audit chain에 기록되어 있음.
type AuditEmitter interface {
	EmitEvidenceStored(ctx context.Context, tx storage.Tx, rec Record) error
}

// Service는 evidence 도메인 진입점입니다 (E7).
type Service interface {
	// Store는 Raw를 redact한 후 blobstore에 영속하고 evidence_records에 메타를 INSERT합니다.
	//
	// 같은 (TenantID, sha256)이 이미 있으면 dedup — 기존 EvidenceID 반환, IsNew=false.
	// 신규 INSERT면 audit emit.
	//
	// Raw가 nil/empty여도 정상 처리 — sha256("") = e3b0c44... blob 1개 생성, 메타 1행.
	Store(ctx context.Context, tx storage.Tx, in StoreInput) (StoreResult, error)

	// Read는 EvidenceID로 메타 + redact된 평문 bytes를 반환합니다.
	// blobstore에서 hash 검증 — mismatch면 ErrBlobCorrupted 매핑.
	Read(ctx context.Context, tx storage.Tx, evidenceID string) (Record, []byte, error)

	// LinkToResult는 (scanResultID, evidenceIDs)를 evidence_refs에 INSERT합니다.
	// 같은 (scan_result_id, evidence_id) 중복은 silently skip(idempotent) — N:M position은
	// 최초 입력 순서를 보존합니다.
	LinkToResult(ctx context.Context, tx storage.Tx, scanResultID string, evidenceIDs []string) ([]RecordedRef, error)

	// ListForResult는 한 scan_result에 붙은 모든 evidence 메타를 position ASC로 반환합니다.
	ListForResult(ctx context.Context, tx storage.Tx, scanResultID string) ([]Record, error)
}

// 공통 에러 sentinel.
var (
	ErrInvalidContentType = errors.New("evidence: invalid content_type")
	ErrEvidenceNotFound   = errors.New("evidence: not found")
	ErrBlobCorrupted      = errors.New("evidence: blob hash mismatch (quarantined)")
	ErrScanResultEmpty    = errors.New("evidence: scan_result_id is empty")
	ErrEvidenceIDsEmpty   = errors.New("evidence: evidence_ids is empty")
)

// ValidContentType은 enum 검증 헬퍼입니다.
func ValidContentType(c ContentType) bool {
	switch c {
	case ContentStdout, ContentStderr, ContentFile, ContentConfigSnapshot, ContentScreenshot:
		return true
	}
	return false
}

// MarshalRedactions는 Redactions 슬라이스를 evidence_records.redactions(JSON) 컬럼에
// 저장할 수 있는 canonical bytes로 직렬화합니다. nil/empty는 "[]" 반환(컬럼 NOT NULL).
func MarshalRedactions(marks []RedactionMark) ([]byte, error) {
	if len(marks) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(marks)
}

// UnmarshalRedactions는 DB 컬럼 bytes를 슬라이스로 역직렬화. empty/nil은 빈 슬라이스.
func UnmarshalRedactions(data []byte) ([]RedactionMark, error) {
	if len(data) == 0 || string(data) == "[]" {
		return nil, nil
	}
	var marks []RedactionMark
	if err := json.Unmarshal(data, &marks); err != nil {
		return nil, err
	}
	return marks, nil
}
