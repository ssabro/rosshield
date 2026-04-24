// Package audit는 감사 도메인의 공개 표면을 정의합니다.
//
// 모든 WRITE 경로는 이 도메인을 통해 append-only 엔트리를 남깁니다 (§10.2).
// 엔트리는 tenant 단조 seq + 해시 체인 + Ed25519 checkpoint 서명으로
// 외부 검증 가능한 무결성을 제공합니다 (§10.4·§10.5).
//
// 어댑터 위치:
//   - sqliterepo: SQLite 저장소 (Phase 1)
//   - 후속(Phase 3+): PostgreSQL, NATS 통합
package audit

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// HashSize는 sha256 출력 크기입니다 (32바이트).
const HashSize = 32

// Hash는 32바이트 sha256 출력입니다.
type Hash [HashSize]byte

// IsZero는 genesis(이전 엔트리 없음) 판정용입니다.
func (h Hash) IsZero() bool {
	for _, b := range h {
		if b != 0 {
			return false
		}
	}
	return true
}

// ActorType은 행위자 분류입니다 (§10.3).
type ActorType string

const (
	ActorUser      ActorType = "user"      // us_...
	ActorAPI       ActorType = "api"       // ak_...
	ActorSystem    ActorType = "system"    // 'system'
	ActorAnonymous ActorType = "anonymous" // 'anonymous' / IP only
)

// Outcome은 행동 결과입니다.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomePartial Outcome = "partial"
)

// Actor는 누가 행위를 했는가입니다.
type Actor struct {
	Type      ActorType
	ID        string
	IP        string // optional
	UserAgent string // optional
}

// Target은 무엇이 영향받았는가입니다.
type Target struct {
	Type string // "robot" | "scan" | "tenant" | ...
	ID   string
}

// ErrorInfo는 outcome != success 시 부가 정보입니다.
type ErrorInfo struct {
	Code    string
	Message string
}

// Entry는 감사 엔트리입니다 (§10.3). 영구 불변.
type Entry struct {
	TenantID      storage.TenantID
	Seq           int64
	OccurredAt    time.Time
	Actor         Actor
	Action        string // "robot.create" | "scan.execute" | ...
	Target        Target
	PayloadDigest Hash
	Outcome       Outcome
	Error         *ErrorInfo // outcome != success 시
	PrevHash      Hash
	Hash          Hash
}

// AppendRequest는 호출자가 Append에 전달하는 입력입니다.
// PayloadDigest는 Service가 Payload bytes에서 sha256으로 계산합니다.
type AppendRequest struct {
	TenantID storage.TenantID
	Actor    Actor
	Action   string
	Target   Target
	Payload  []byte // canonical JSON 등 직렬화된 변경 본문 (선택). 비어 있어도 됨.
	Outcome  Outcome
	Error    *ErrorInfo
}

// ChainHead는 테넌트당 1행입니다 (§10.4).
type ChainHead struct {
	TenantID  storage.TenantID
	Seq       int64
	Hash      Hash
	UpdatedAt time.Time
}

// VerifyResult는 Verify의 출력입니다.
//
// OK=true면 fromSeq~toSeq 모든 엔트리가 무결성 검사를 통과했습니다.
// OK=false면 BreakAt이 처음 깨진 seq를 표시하고 Reason이 사람 읽기용 설명입니다.
type VerifyResult struct {
	OK             bool   // true면 클린 체인.
	BreakAt        int64  // OK=false일 때 첫 위반 seq. OK=true면 0.
	Reason         string // 위반 종류·위치 설명. OK=true면 빈 문자열.
	EntriesScanned int64  // 실제로 검증한 엔트리 수.
}

// Service는 감사 도메인의 진입점입니다.
//
// Append는 외부 트랜잭션을 받아서 도메인 변경과 같은 Tx에 묶입니다 (P5).
// 호출자는 도메인 INSERT와 audit Append를 동일 Tx에 두어 원자성을 보장합니다.
type Service interface {
	// Append는 새 엔트리를 추가합니다.
	// tx는 storage.Storage.Tx에서 받은 것이어야 하며, tenant scope가 일치해야 합니다.
	// req.TenantID와 tx.TenantID()가 다르면 ErrTenantMismatch.
	Append(ctx context.Context, tx storage.Tx, req AppendRequest) (Entry, error)

	// Head는 tenant의 현재 chain head를 반환합니다.
	// head가 없으면 (TenantID만 채운, Seq=0, Hash=zero) genesis head 반환.
	Head(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) (ChainHead, error)

	// Verify는 fromSeq~toSeq 범위 엔트리의 해시 체인 무결성을 재계산하여 검증합니다.
	// fromSeq <= 0이면 1로 보정. toSeq <= 0 또는 toSeq < fromSeq면 head.Seq까지 검증.
	// 첫 위반 시점에 OK=false + BreakAt + Reason을 채우고 즉시 반환합니다 (early termination).
	Verify(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fromSeq, toSeq int64) (VerifyResult, error)

	// Export는 fromSeq~toSeq 엔트리를 NDJSON+gzip으로 내보내고 마지막 라인에 SIGNATURE 메타를 추가합니다.
	// fromSeq <= 0 → 1, toSeq <= 0 → head.Seq.
	// 외부 검증 도구(`fg-verify` OSS, §10.6)는 이 스트림을 받아 chain 재계산 + signer.Verify(공개키)로 무결성 확인.
	//
	// 호출자는 반환된 ReadCloser에서 모두 읽은 후 Close해야 합니다.
	Export(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fromSeq, toSeq int64, sgn signer.Signer) (io.ReadCloser, error)
}

// 공통 에러.
var (
	ErrTenantMismatch = errors.New("audit: req.TenantID does not match tx.TenantID")
	ErrEmptyAction    = errors.New("audit: Action is required")
	ErrEmptyTarget    = errors.New("audit: Target.Type and Target.ID are required")
	ErrInvalidActor   = errors.New("audit: Actor.Type is not a known value")
	ErrInvalidOutcome = errors.New("audit: Outcome is not a known value")
)
