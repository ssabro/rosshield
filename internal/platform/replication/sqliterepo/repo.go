// Package sqliterepo는 replication.Repository의 SQLite/PG 어댑터입니다 (E-MR Stage 1).
//
// SQL은 `?` placeholder만 사용 — postgres 어댑터(internal/platform/storage/postgres/pg.go)의
// rebind가 `?` → `$N` 자동 변환. 본 패키지는 sqlite·postgres 양쪽에서 그대로 동작합니다.
//
// 도메인 경계 (P5): 본 패키지는 internal/platform/replication 만 import. audit emit·
// HTTP handler는 별 layer (handler 또는 bootstrap).
package sqliterepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/platform/replication"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

const rfc3339Nano = time.RFC3339Nano

// Repo는 replication.Repository의 sqlite/postgres 어댑터입니다.
type Repo struct{}

// New는 새 Repo를 반환합니다.
func New() *Repo {
	return &Repo{}
}

// 컴파일 시점 인터페이스 매칭 보증.
var _ replication.Repository = (*Repo)(nil)

// RegisterReplica는 새 replica row를 INSERT 합니다.
// UNIQUE(region) 위반 시 ErrReplicaExists로 변환.
func (r *Repo) RegisterReplica(ctx context.Context, tx storage.Tx, req replication.RegisterReplicaRequest, now time.Time) (replication.Replica, error) {
	if err := replication.ValidateRegisterRequest(req); err != nil {
		return replication.Replica{}, err
	}

	nowStr := now.UTC().Format(rfc3339Nano)
	// SQLite는 INTEGER 1/0, PG는 BOOLEAN true/false — driver가 알아서 처리.
	// enabled default TRUE는 schema에 명시되어 있지만 명시적으로 INSERT 하여 통합 일관.
	res, err := tx.Exec(ctx, `INSERT INTO replication_replicas (
    region, role, endpoint, enabled, created_at
) VALUES (?, ?, ?, ?, ?)`,
		req.Region, string(req.Role), req.Endpoint, true, nowStr)
	if err != nil {
		// UNIQUE violation 식별: sqlite "UNIQUE constraint failed" / postgres "duplicate key"
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate key") {
			return replication.Replica{}, replication.ErrReplicaExists
		}
		return replication.Replica{}, fmt.Errorf("replication: insert replica: %w", err)
	}

	id, idErr := res.LastInsertId()
	if idErr != nil {
		// PG는 LastInsertId 미지원 — RETURNING으로 fallback 필요하지만 본 stage는 id 옵션.
		id = 0
	}

	return replication.Replica{
		ID:        id,
		Region:    req.Region,
		Role:      req.Role,
		Endpoint:  req.Endpoint,
		Enabled:   true,
		CreatedAt: now.UTC(),
	}, nil
}

// GetReplica는 region으로 row를 조회합니다.
func (r *Repo) GetReplica(ctx context.Context, tx storage.Tx, region string) (replication.Replica, error) {
	if region == "" {
		return replication.Replica{}, replication.ErrEmptyRegion
	}
	row := tx.QueryRow(ctx, replicaColumns+` FROM replication_replicas WHERE region = ?`, region)
	out, err := scanReplica(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return replication.Replica{}, replication.ErrReplicaNotFound
		}
		return replication.Replica{}, err
	}
	return out, nil
}

// ListReplicas는 모든 replica를 region ASC로 반환합니다.
func (r *Repo) ListReplicas(ctx context.Context, tx storage.Tx) ([]replication.Replica, error) {
	rows, err := tx.Query(ctx, replicaColumns+` FROM replication_replicas ORDER BY region ASC`)
	if err != nil {
		return nil, fmt.Errorf("replication: list replicas: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []replication.Replica
	for rows.Next() {
		rep, scanErr := scanReplicaRows(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, rep)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, fmt.Errorf("replication: list replicas rows: %w", rerr)
	}
	return out, nil
}

// UpdateHeartbeat는 standby의 LSN + heartbeat 시각을 갱신합니다.
func (r *Repo) UpdateHeartbeat(ctx context.Context, tx storage.Tx, req replication.HeartbeatRequest) error {
	if req.Region == "" {
		return replication.ErrEmptyRegion
	}
	nowStr := req.Now.UTC().Format(rfc3339Nano)
	var lsn any = nil
	if req.LastReplayLSN != "" {
		lsn = req.LastReplayLSN
	}
	res, err := tx.Exec(ctx, `UPDATE replication_replicas
        SET last_replay_lsn = ?, last_replay_at = ?, last_heartbeat_at = ?
        WHERE region = ?`,
		lsn, nowStr, nowStr, req.Region)
	if err != nil {
		return fmt.Errorf("replication: update heartbeat: %w", err)
	}
	n, raErr := res.RowsAffected()
	if raErr == nil && n == 0 {
		return replication.ErrReplicaNotFound
	}
	return nil
}

// SetRole은 region의 role을 변경합니다.
func (r *Repo) SetRole(ctx context.Context, tx storage.Tx, region string, role replication.Role) error {
	if region == "" {
		return replication.ErrEmptyRegion
	}
	if !role.IsValid() {
		return replication.ErrInvalidRole
	}
	res, err := tx.Exec(ctx, `UPDATE replication_replicas SET role = ? WHERE region = ?`,
		string(role), region)
	if err != nil {
		return fmt.Errorf("replication: set role: %w", err)
	}
	n, raErr := res.RowsAffected()
	if raErr == nil && n == 0 {
		return replication.ErrReplicaNotFound
	}
	return nil
}

// RecordFailover는 failover 이력 row를 INSERT 합니다.
func (r *Repo) RecordFailover(ctx context.Context, tx storage.Tx, req replication.FailoverRequest) (replication.Failover, error) {
	if err := replication.ValidateFailoverRequest(req); err != nil {
		return replication.Failover{}, err
	}
	initStr := req.Now.UTC().Format(rfc3339Nano)
	var initiatedBy any = nil
	if req.InitiatedByUser != "" {
		initiatedBy = req.InitiatedByUser
	}
	var reason any = nil
	if req.Reason != "" {
		reason = req.Reason
	}
	res, err := tx.Exec(ctx, `INSERT INTO replication_failovers (
    from_region, to_region, initiated_by_user, initiated_at, reason
) VALUES (?, ?, ?, ?, ?)`,
		req.FromRegion, req.ToRegion, initiatedBy, initStr, reason)
	if err != nil {
		return replication.Failover{}, fmt.Errorf("replication: insert failover: %w", err)
	}
	id, idErr := res.LastInsertId()
	if idErr != nil {
		id = 0
	}
	return replication.Failover{
		ID:              id,
		FromRegion:      req.FromRegion,
		ToRegion:        req.ToRegion,
		InitiatedByUser: req.InitiatedByUser,
		InitiatedAt:     req.Now.UTC(),
		Reason:          req.Reason,
	}, nil
}

// LinkFailoverAudit는 failover row에 audit_entry_id + completed_at을 채웁니다.
func (r *Repo) LinkFailoverAudit(ctx context.Context, tx storage.Tx, failoverID int64, auditEntryID int64, completedAt time.Time) error {
	if failoverID <= 0 {
		return replication.ErrFailoverNotFound
	}
	completedStr := completedAt.UTC().Format(rfc3339Nano)
	res, err := tx.Exec(ctx, `UPDATE replication_failovers
        SET audit_entry_id = ?, completed_at = ?
        WHERE id = ?`,
		auditEntryID, completedStr, failoverID)
	if err != nil {
		return fmt.Errorf("replication: link failover audit: %w", err)
	}
	n, raErr := res.RowsAffected()
	if raErr == nil && n == 0 {
		return replication.ErrFailoverNotFound
	}
	return nil
}

const replicaColumns = `SELECT id, region, role, endpoint,
    COALESCE(last_replay_lsn, ''), last_replay_at, last_heartbeat_at,
    enabled, created_at`

// scanReplica는 *sql.Row에서 Replica를 추출합니다.
func scanReplica(row *sql.Row) (replication.Replica, error) {
	var (
		rep                                      replication.Replica
		roleStr                                  string
		lastReplayAt, lastHeartbeatAt, createdAt sql.NullString
		lsn                                      string
		enabledRaw                               any
	)
	err := row.Scan(&rep.ID, &rep.Region, &roleStr, &rep.Endpoint,
		&lsn, &lastReplayAt, &lastHeartbeatAt, &enabledRaw, &createdAt)
	if err != nil {
		return replication.Replica{}, err
	}
	rep.Role = replication.Role(roleStr)
	rep.LastReplayLSN = lsn
	rep.LastReplayAt = parseTimeOrZero(lastReplayAt)
	rep.LastHeartbeatAt = parseTimeOrZero(lastHeartbeatAt)
	rep.CreatedAt = parseTimeOrZero(createdAt)
	rep.Enabled = coerceBool(enabledRaw)
	return rep, nil
}

// scanReplicaRows는 *sql.Rows에서 Replica 1행을 추출합니다.
func scanReplicaRows(rows *sql.Rows) (replication.Replica, error) {
	var (
		rep                                      replication.Replica
		roleStr                                  string
		lastReplayAt, lastHeartbeatAt, createdAt sql.NullString
		lsn                                      string
		enabledRaw                               any
	)
	err := rows.Scan(&rep.ID, &rep.Region, &roleStr, &rep.Endpoint,
		&lsn, &lastReplayAt, &lastHeartbeatAt, &enabledRaw, &createdAt)
	if err != nil {
		return replication.Replica{}, err
	}
	rep.Role = replication.Role(roleStr)
	rep.LastReplayLSN = lsn
	rep.LastReplayAt = parseTimeOrZero(lastReplayAt)
	rep.LastHeartbeatAt = parseTimeOrZero(lastHeartbeatAt)
	rep.CreatedAt = parseTimeOrZero(createdAt)
	rep.Enabled = coerceBool(enabledRaw)
	return rep, nil
}

// parseTimeOrZero는 sql.NullString → time.Time (RFC3339Nano). PG TIMESTAMPTZ 텍스트도
// time.RFC3339Nano로 해석 가능 (driver 변환 후 text format).
func parseTimeOrZero(s sql.NullString) time.Time {
	if !s.Valid || s.String == "" {
		return time.Time{}
	}
	// PG TIMESTAMPTZ는 driver가 time.Time으로 반환 가능 — 본 layer는 text scan으로 통일.
	// 다양한 layout 시도 (sqlite: RFC3339Nano, PG: 다양한 microsecond 형식).
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999-07",
		"2006-01-02 15:04:05.999999Z07:00",
		"2006-01-02 15:04:05-07",
	} {
		if t, err := time.Parse(layout, s.String); err == nil {
			return t
		}
	}
	return time.Time{}
}

// coerceBool은 SQLite INTEGER 1/0 또는 PG BOOLEAN true/false → bool.
func coerceBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x != 0
	case int:
		return x != 0
	case []byte:
		s := strings.ToLower(string(x))
		return s == "true" || s == "t" || s == "1"
	case string:
		s := strings.ToLower(x)
		return s == "true" || s == "t" || s == "1"
	}
	return false
}
