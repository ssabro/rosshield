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
	// LeaderEpoch는 E25 HA fence token입니다. nil이면 HA 비활성 상태에서 INSERT됨.
	// 양수면 INSERT 시점의 leader_epoch.current=1 row epoch과 일치 — 향후 stale write
	// 검증·split-brain 분석에 사용.
	LeaderEpoch *int64

	// KeyEpoch 는 Phase 10.D-4 chain key rotation epoch 입니다. nil 이면 SwappableSigner
	// 미주입 또는 마이그레이션 이전 INSERT. 양수면 INSERT 시점의 SwappableSigner.CurrentEpoch()
	// 와 일치 — audit_chain_keys.epoch 와 매칭.
	KeyEpoch *int64
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

// Checkpoint는 특정 시점의 head 상태에 대한 외부 서명입니다 (§10.5).
//
// 서명 payload: SerializeCheckpointPayload(tenantID, seq, hash).
// 외부 검증 도구는 동일한 payload를 재구성하여 signer.Verify(publicKey, payload, signature)로 무결성 확인.
type Checkpoint struct {
	TenantID    storage.TenantID
	Seq         int64
	Hash        Hash
	SignedAt    time.Time
	SignerKeyID string
	Signature   []byte // Ed25519 64B
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

	// WriteCheckpoint는 tenant의 현재 head를 Ed25519로 서명하여 audit_checkpoints에 INSERT합니다.
	// head.Seq == 0 (빈 체인)이면 ErrNoEntries — 호출자(cron)는 no-op으로 처리.
	// 동일 (tenant, seq)에 이미 checkpoint가 있으면 ErrCheckpointExists — 새 entry 추가 전에는 의미 없음.
	WriteCheckpoint(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, sgn signer.Signer) (Checkpoint, error)

	// LatestCheckpoint는 tenant의 가장 최근 checkpoint를 반환합니다. 없으면 storage.ErrNotFound.
	LatestCheckpoint(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) (Checkpoint, error)
}

// 공통 에러.
var (
	ErrTenantMismatch   = errors.New("audit: req.TenantID does not match tx.TenantID")
	ErrEmptyAction      = errors.New("audit: Action is required")
	ErrEmptyTarget      = errors.New("audit: Target.Type and Target.ID are required")
	ErrInvalidActor     = errors.New("audit: Actor.Type is not a known value")
	ErrInvalidOutcome   = errors.New("audit: Outcome is not a known value")
	ErrNoEntries        = errors.New("audit: chain has no entries to checkpoint")
	ErrCheckpointExists = errors.New("audit: checkpoint already exists for this seq")

	// ErrNotLeader는 HA 활성 환경에서 follower 인스턴스가 Append를 시도할 때 반환됩니다.
	// API middleware는 이를 503 Service Unavailable + NOT_LEADER 코드로 매핑합니다 (Stage 3).
	ErrNotLeader = errors.New("audit: instance is not leader (HA single-writer)")
)

// ActionCountWindow 는 Phase 11.B-6 effectiveness dashboard 가 audit_entries 를
// action × time-range 로 집계할 때 사용하는 결과 row 입니다.
//
// 각 윈도우(LastDay/Last7Days/Last30Days)는 occurred_at >= 윈도우 시작 시각의 카운트.
// 매핑되지 않은 action 은 결과에 포함되지 않습니다 — caller (handler) 가 IN (...) 절로
// 제한.
type ActionCountWindow struct {
	Action     string
	LastDay    int64
	Last7Days  int64
	Last30Days int64
}

// EffectivenessAggregator 는 Phase 11.B-6 effectiveness dashboard 가 의존하는
// minimal read-only 표면입니다 (P5 minimal DTO).
//
// 본 표면은 audit.Service 와 분리되어 있습니다 — Service 는 광범위하게 주입되고,
// 본 표면은 effectiveness handler 만 사용하므로 작게 격리합니다.
//
// 구현: internal/domain/audit/sqliterepo.Repo (이미 audit_entries 접근 권한 보유).
// bootstrap 이 audit/sqliterepo.Repo 인스턴스를 그대로 본 interface 로 주입.
//
// 권한 경계: caller (handler) 는 tenant 컨텍스트 + audit.export 권한 보유 후 호출
// (permission_matrix.go §3.3 — admin + auditor).
type EffectivenessAggregator interface {
	// CountActionsByWindows 는 tenant 의 audit_entries 에서 (action, time-range) 별 카운트를
	// 한 번의 query 로 회수합니다.
	//
	// actions 가 empty 면 빈 슬라이스 반환 (no-op). reference 시각 now 로부터 1/7/30 일 윈도우.
	// 결과 슬라이스는 actions 와 동일 길이 + 동일 순서 — caller 가 zip 으로 매핑.
	//
	// 미발견 action 은 카운트 0 으로 채워 반환 (절대 누락 안 함).
	CountActionsByWindows(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, actions []string, now time.Time) ([]ActionCountWindow, error)
}

// RoleProvider는 audit가 HA 활성 시 leader 여부 + 현재 fence token(epoch)을
// 질의할 수 있는 minimal interface입니다.
//
// nil 가능 — 그 경우 HA 비활성으로 간주하고 모든 Append를 leader-gate 없이 통과시킵니다.
// platform/ha.Manager가 본 interface를 자동 만족 (duck typing — audit는 ha 패키지 미import).
type RoleProvider interface {
	IsLeader() bool
	CurrentEpoch() int64
}

// KeyEpochProvider 는 Phase 10.D-4 audit chain key rotation 활성 epoch 를 질의할 수 있는
// minimal interface 입니다.
//
// nil 가능 — 그 경우 audit_entries.key_epoch 컬럼은 NULL.
// platform/signer.SwappableSigner 가 본 interface 를 자동 만족 (duck typing —
// audit 는 signer 패키지 미import 가드 일관).
type KeyEpochProvider interface {
	CurrentEpoch() int64
}
