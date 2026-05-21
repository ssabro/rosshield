package replication

import (
	"context"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Replica는 replication_replicas 테이블의 도메인 표현입니다.
//
// region별 1행 (UNIQUE(region)). role=primary는 deployment 내 정확히 1개여야 합니다 —
// 강제는 application logic 책임 (failover handler가 swap 시 정확히 한 트랜잭션에서
// 둘 다 UPDATE).
type Replica struct {
	ID              int64
	Region          string
	Role            Role
	Endpoint        string
	LastReplayLSN   string // PG LSN 텍스트 형식 "X/X" (옵션)
	LastReplayAt    time.Time
	LastHeartbeatAt time.Time
	Enabled         bool
	CreatedAt       time.Time
}

// Failover는 replication_failovers 테이블의 도메인 표현입니다.
type Failover struct {
	ID              int64
	FromRegion      string
	ToRegion        string
	InitiatedByUser string // admin user id (UUID 텍스트)
	InitiatedAt     time.Time
	CompletedAt     time.Time
	Reason          string
	AuditEntryID    int64 // audit.replication.failover entry soft link
}

// RegisterReplicaRequest는 replica 등록 입력입니다.
type RegisterReplicaRequest struct {
	Region   string
	Role     Role
	Endpoint string
}

// HeartbeatRequest는 standby가 primary에 ping 보낼 때 입력입니다.
//
// LSN은 PG `pg_last_wal_replay_lsn()` 텍스트 형식 그대로 (예: "0/19000060").
// 빈 값 허용 (PG 외 환경).
type HeartbeatRequest struct {
	Region        string
	LastReplayLSN string
	Now           time.Time
}

// FailoverRequest는 manual failover 입력입니다.
//
// FromRegion·ToRegion·InitiatedByUser는 필수. Reason은 자유 텍스트(옵션, 권장).
type FailoverRequest struct {
	FromRegion      string
	ToRegion        string
	InitiatedByUser string
	Reason          string
	Now             time.Time
}

// Repository는 replication 메타데이터 영속화 진입점입니다.
//
// 모든 메서드는 외부 트랜잭션을 받습니다 (P5 — 도메인 logic이 audit emit과 같은 Tx에
// 묶이도록). Bootstrap Tx로 호출 (replication 메타는 tenant 글로벌 — 인프라 토폴로지).
type Repository interface {
	// RegisterReplica는 새 replica를 INSERT 합니다 (UNIQUE(region) 위반 시 ErrReplicaExists).
	RegisterReplica(ctx context.Context, tx storage.Tx, req RegisterReplicaRequest, now time.Time) (Replica, error)

	// GetReplica는 region으로 replica를 조회합니다.
	GetReplica(ctx context.Context, tx storage.Tx, region string) (Replica, error)

	// ListReplicas는 모든 replica를 region ASC로 반환합니다.
	ListReplicas(ctx context.Context, tx storage.Tx) ([]Replica, error)

	// UpdateHeartbeat는 standby의 LSN + heartbeat 시각을 갱신합니다.
	// region이 없으면 ErrReplicaNotFound.
	UpdateHeartbeat(ctx context.Context, tx storage.Tx, req HeartbeatRequest) error

	// SetRole은 region의 role을 primary 또는 standby로 변경합니다.
	// failover handler가 swap 시 두 region에 대해 차례로 호출 — 둘 다 같은 Tx에 묶음.
	SetRole(ctx context.Context, tx storage.Tx, region string, role Role) error

	// RecordFailover는 failover 이력을 INSERT 하고 새 ID를 반환합니다.
	// audit_entry_id는 후속 UPDATE로 link (audit append 후 LinkFailoverAudit).
	RecordFailover(ctx context.Context, tx storage.Tx, req FailoverRequest) (Failover, error)

	// LinkFailoverAudit는 RecordFailover로 만든 row에 audit_entry_id를 채웁니다.
	// 같은 Tx에서 audit.Service.Append → 반환된 Entry.Seq(또는 row id)를 link.
	LinkFailoverAudit(ctx context.Context, tx storage.Tx, failoverID int64, auditEntryID int64, completedAt time.Time) error

	// ListFailovers는 가장 최근 failover 이력을 initiated_at DESC로 N개 반환합니다.
	// Phase 10.A-4 `/regions` 페이지 RegionTimelineCard용 — read-only 이력 표시.
	// limit ≤ 0이면 default(50) 사용. 최대 200으로 cap.
	ListFailovers(ctx context.Context, tx storage.Tx, limit int) ([]Failover, error)
}

// 공통 에러.
var (
	ErrReplicaExists      = errors.New("replication: replica already exists for region")
	ErrReplicaNotFound    = errors.New("replication: replica not found")
	ErrFailoverNotFound   = errors.New("replication: failover row not found")
	ErrInvalidRole        = errors.New("replication: invalid role (allowed: primary|standby)")
	ErrEmptyRegion        = errors.New("replication: region is required")
	ErrEmptyEndpoint      = errors.New("replication: endpoint is required")
	ErrSameRegionFailover = errors.New("replication: from_region and to_region must differ")
)

// ValidateRegisterRequest는 RegisterReplicaRequest의 필드를 검증합니다.
func ValidateRegisterRequest(req RegisterReplicaRequest) error {
	if req.Region == "" {
		return ErrEmptyRegion
	}
	if req.Endpoint == "" {
		return ErrEmptyEndpoint
	}
	if !req.Role.IsValid() {
		return ErrInvalidRole
	}
	return nil
}

// ValidateFailoverRequest는 FailoverRequest의 필드를 검증합니다.
func ValidateFailoverRequest(req FailoverRequest) error {
	if req.FromRegion == "" || req.ToRegion == "" {
		return ErrEmptyRegion
	}
	if req.FromRegion == req.ToRegion {
		return ErrSameRegionFailover
	}
	return nil
}
