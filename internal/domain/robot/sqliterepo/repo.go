// Package sqliterepo는 robot.Service의 SQLite 어댑터입니다 (E5).
//
// Stage A는 Fleet CRUD만 구현. 후속 Stage:
//
//	Stage B — Credential KEK/DEK + 마이그레이션 0009 (별도 메서드).
//	Stage C — Robot CRUD + 마이그레이션 0010 (CreateRobot은 한 Tx에 Robot+Credential 묶음).
//	Stage D — CSV import.
//	Stage E — TestConnection mock.
package sqliterepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// rfc3339Nano는 DB의 시간 칼럼 직렬화 포맷입니다 (E2·E3 동일).
const rfc3339Nano = time.RFC3339Nano

// maxFleetNameLen은 fleet name의 길이 상한입니다.
const maxFleetNameLen = 200

// Deps는 어댑터 의존성입니다.
type Deps struct {
	Clock clock.Clock
	IDGen idgen.IDGen
	Audit robot.AuditEmitter // bootstrap이 audit.Service를 어댑팅한 구현체 주입.

	// KEK는 Credential wrap/unwrap에 사용 (Stage B 도입).
	// bootstrap이 부팅 시 LoadOrCreate해 주입.
	KEK *robot.KEK

	// SSHTester는 Service.TestConnection이 위임하는 SSH 연결 테스트 표면 (Stage E).
	// nil이면 TestConnection 호출 시 ErrSSHTesterNotConfigured.
	// 실제 구현은 E6 sshpool에서 등장 — 그 전까지 bootstrap에서 nil로 주입.
	SSHTester robot.SSHTester
}

// Repo는 robot.Service의 SQLite 구현입니다.
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// CreateFleet는 robot.Service.CreateFleet 구현입니다.
//
// 한 Tx에 fleet INSERT + audit emit. ctx의 TenantID로 격리. 빈 TenantID면 ErrTenantMissing.
// 같은 tenant 내 활성 fleet 이름 중복 시 ErrFleetNameDuplicate.
func (r *Repo) CreateFleet(ctx context.Context, tx storage.Tx, req robot.CreateFleetRequest) (robot.Fleet, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return robot.Fleet{}, storage.ErrTenantMissing
	}
	if err := validateFleetName(req.Name); err != nil {
		return robot.Fleet{}, err
	}
	if err := validatePolicy(req.Policy); err != nil {
		return robot.Fleet{}, err
	}

	now := r.deps.Clock.Now().UTC()
	policyJSON, err := robot.MarshalPolicy(req.Policy)
	if err != nil {
		return robot.Fleet{}, fmt.Errorf("robot: marshal policy: %w", err)
	}

	fleet := robot.Fleet{
		ID:          r.deps.IDGen.New("fl"),
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Policy:      req.Policy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := insertFleet(ctx, tx, fleet, policyJSON); err != nil {
		if isUniqueViolation(err) {
			return robot.Fleet{}, robot.ErrFleetNameDuplicate
		}
		return robot.Fleet{}, fmt.Errorf("robot: insert fleet: %w", err)
	}

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitFleetCreated(ctx, tx, fleet); err != nil {
			return robot.Fleet{}, fmt.Errorf("robot: emit audit: %w", err)
		}
	}

	return fleet, nil
}

// GetFleet는 robot.Service.GetFleet 구현입니다 (활성만, deleted_at IS NULL).
//
// RobotCount derived field — 활성 로봇 수를 sub-query로 함께 SELECT (단일 trip).
func (r *Repo) GetFleet(ctx context.Context, tx storage.Tx, id string) (robot.Fleet, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return robot.Fleet{}, storage.ErrTenantMissing
	}
	row := tx.QueryRow(ctx, `
SELECT f.id, f.tenant_id, f.name, f.description, f.policy, f.created_at, f.updated_at, f.deleted_at,
       (SELECT COUNT(*) FROM robots r
          WHERE r.fleet_id = f.id AND r.tenant_id = f.tenant_id AND r.deleted_at IS NULL) AS robot_count
  FROM fleets f
 WHERE f.id = ? AND f.tenant_id = ? AND f.deleted_at IS NULL`,
		id, string(tenantID))
	return scanFleetRow(row)
}

// ListFleets는 robot.Service.ListFleets 구현입니다 (활성만, name ASC).
//
// name ASC: 운영자가 dropdown에서 알파벳순 탐색하기 쉬움. created_at 순서가 필요한 호출자는
// 본 결과를 직접 정렬. RobotCount는 활성 로봇 수 sub-query (단일 trip).
func (r *Repo) ListFleets(ctx context.Context, tx storage.Tx) ([]robot.Fleet, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	rows, err := tx.Query(ctx, `
SELECT f.id, f.tenant_id, f.name, f.description, f.policy, f.created_at, f.updated_at, f.deleted_at,
       (SELECT COUNT(*) FROM robots r
          WHERE r.fleet_id = f.id AND r.tenant_id = f.tenant_id AND r.deleted_at IS NULL) AS robot_count
  FROM fleets f
 WHERE f.tenant_id = ? AND f.deleted_at IS NULL
 ORDER BY f.name ASC`,
		string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("robot: list fleets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []robot.Fleet
	for rows.Next() {
		f, err := scanFleetRowGeneric(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("robot: list fleets iterate: %w", err)
	}
	return out, nil
}

// UpdateFleet은 fleet name·description·policy를 수정합니다 (옵션 필드만).
//
// req.Policy nil이면 미변경, non-nil이면 통째 교체. validatePolicy로 enum 검증.
func (r *Repo) UpdateFleet(ctx context.Context, tx storage.Tx, id string, req robot.UpdateFleetRequest) (robot.Fleet, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return robot.Fleet{}, storage.ErrTenantMissing
	}
	current, err := r.GetFleet(ctx, tx, id)
	if err != nil {
		return robot.Fleet{}, err
	}

	updated := current
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if err := validateFleetName(name); err != nil {
			return robot.Fleet{}, err
		}
		updated.Name = name
	}
	if req.Description != nil {
		updated.Description = strings.TrimSpace(*req.Description)
	}
	if req.Policy != nil {
		if err := validatePolicy(*req.Policy); err != nil {
			return robot.Fleet{}, err
		}
		updated.Policy = *req.Policy
	}

	policyEqual := updated.Policy == current.Policy
	if updated.Name == current.Name && updated.Description == current.Description && policyEqual {
		return current, nil // no-op
	}
	updated.UpdatedAt = r.deps.Clock.Now().UTC()

	policyJSON, err := robot.MarshalPolicy(updated.Policy)
	if err != nil {
		return robot.Fleet{}, fmt.Errorf("robot: marshal policy: %w", err)
	}

	if _, err := tx.Exec(ctx, `
UPDATE fleets SET name = ?, description = ?, policy = ?, updated_at = ?
 WHERE id = ? AND tenant_id = ? AND deleted_at IS NULL`,
		updated.Name, updated.Description, string(policyJSON), updated.UpdatedAt.Format(rfc3339Nano),
		id, string(tenantID),
	); err != nil {
		if isUniqueViolation(err) {
			return robot.Fleet{}, robot.ErrFleetNameDuplicate
		}
		return robot.Fleet{}, fmt.Errorf("robot: update fleet: %w", err)
	}

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitFleetUpdated(ctx, tx, updated); err != nil {
			return robot.Fleet{}, fmt.Errorf("robot: emit fleet updated: %w", err)
		}
	}
	return updated, nil
}

// DeleteFleet은 fleet을 soft delete합니다 (deleted_at = now).
func (r *Repo) DeleteFleet(ctx context.Context, tx storage.Tx, id string) error {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return storage.ErrTenantMissing
	}
	current, err := r.GetFleet(ctx, tx, id)
	if err != nil {
		return err
	}

	now := r.deps.Clock.Now().UTC().Format(rfc3339Nano)
	res, err := tx.Exec(ctx, `
UPDATE fleets SET deleted_at = ?, updated_at = ?
 WHERE id = ? AND tenant_id = ? AND deleted_at IS NULL`,
		now, now, id, string(tenantID))
	if err != nil {
		return fmt.Errorf("robot: delete fleet: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitFleetDeleted(ctx, tx, current); err != nil {
			return fmt.Errorf("robot: emit fleet deleted: %w", err)
		}
	}
	return nil
}

// insertFleet은 INSERT 쿼리를 실행합니다.
func insertFleet(ctx context.Context, tx storage.Tx, f robot.Fleet, policyJSON []byte) error {
	_, err := tx.Exec(ctx, `
INSERT INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at, deleted_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`,
		f.ID, string(f.TenantID), f.Name, f.Description,
		string(policyJSON), f.CreatedAt.Format(rfc3339Nano), f.UpdatedAt.Format(rfc3339Nano))
	return err
}

// scanFleetRow는 *sql.Row를 Fleet으로 디코드합니다 (마지막 컬럼은 robot_count).
func scanFleetRow(row *sql.Row) (robot.Fleet, error) {
	var (
		id, tenantID, name, description, policy, createdAt, updatedAt string
		deletedAt                                                     sql.NullString
		robotCount                                                    int
	)
	if err := row.Scan(&id, &tenantID, &name, &description, &policy, &createdAt, &updatedAt, &deletedAt, &robotCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return robot.Fleet{}, storage.ErrNotFound
		}
		return robot.Fleet{}, fmt.Errorf("robot: scan fleet: %w", err)
	}
	return assembleFleet(id, tenantID, name, description, policy, createdAt, updatedAt, deletedAt, robotCount)
}

// scanFleetRowGeneric은 *sql.Rows를 Fleet으로 디코드합니다 (List에서 사용, 마지막 컬럼은 robot_count).
func scanFleetRowGeneric(rows *sql.Rows) (robot.Fleet, error) {
	var (
		id, tenantID, name, description, policy, createdAt, updatedAt string
		deletedAt                                                     sql.NullString
		robotCount                                                    int
	)
	if err := rows.Scan(&id, &tenantID, &name, &description, &policy, &createdAt, &updatedAt, &deletedAt, &robotCount); err != nil {
		return robot.Fleet{}, fmt.Errorf("robot: scan fleet: %w", err)
	}
	return assembleFleet(id, tenantID, name, description, policy, createdAt, updatedAt, deletedAt, robotCount)
}

func assembleFleet(id, tenantID, name, description, policy, createdAt, updatedAt string, deletedAt sql.NullString, robotCount int) (robot.Fleet, error) {
	created, err := time.Parse(rfc3339Nano, createdAt)
	if err != nil {
		return robot.Fleet{}, fmt.Errorf("robot: parse created_at %q: %w", createdAt, err)
	}
	updated, err := time.Parse(rfc3339Nano, updatedAt)
	if err != nil {
		return robot.Fleet{}, fmt.Errorf("robot: parse updated_at %q: %w", updatedAt, err)
	}
	pol, err := robot.UnmarshalPolicy([]byte(policy))
	if err != nil {
		return robot.Fleet{}, fmt.Errorf("robot: unmarshal policy: %w", err)
	}
	f := robot.Fleet{
		ID:          id,
		TenantID:    storage.TenantID(tenantID),
		Name:        name,
		Description: description,
		Policy:      pol,
		CreatedAt:   created,
		UpdatedAt:   updated,
		RobotCount:  robotCount,
	}
	if deletedAt.Valid {
		t, err := time.Parse(rfc3339Nano, deletedAt.String)
		if err != nil {
			return robot.Fleet{}, fmt.Errorf("robot: parse deleted_at %q: %w", deletedAt.String, err)
		}
		f.DeletedAt = &t
	}
	return f, nil
}

func validateFleetName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return robot.ErrFleetEmptyName
	}
	if len(trimmed) > maxFleetNameLen {
		return robot.ErrFleetNameTooLong
	}
	return nil
}

func validatePolicy(p robot.FleetPolicy) error {
	if p.DefaultLevel != "" && p.DefaultLevel != robot.LevelL1 && p.DefaultLevel != robot.LevelL2 {
		return robot.ErrFleetInvalidLevel
	}
	switch p.DefaultCriticality {
	case "", robot.CriticalityLow, robot.CriticalityMedium, robot.CriticalityHigh, robot.CriticalityCritical:
		return nil
	default:
		return robot.ErrFleetInvalidCritical
	}
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
