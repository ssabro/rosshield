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
	// Stage B 시점엔 robotSvc 내부에서 직접 사용하지 않지만(Stage C에서 CreateRobot 일부로),
	// bootstrap이 부팅 시 LoadOrCreate해 주입 — Phase 1 운영 표면 연결.
	KEK *robot.KEK
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
func (r *Repo) GetFleet(ctx context.Context, tx storage.Tx, id string) (robot.Fleet, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return robot.Fleet{}, storage.ErrTenantMissing
	}
	row := tx.QueryRow(ctx, `
SELECT id, tenant_id, name, description, policy, created_at, updated_at, deleted_at
  FROM fleets
 WHERE id = ? AND tenant_id = ? AND deleted_at IS NULL`,
		id, string(tenantID))
	return scanFleetRow(row)
}

// ListFleets는 robot.Service.ListFleets 구현입니다 (활성만, 생성순).
func (r *Repo) ListFleets(ctx context.Context, tx storage.Tx) ([]robot.Fleet, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	rows, err := tx.Query(ctx, `
SELECT id, tenant_id, name, description, policy, created_at, updated_at, deleted_at
  FROM fleets
 WHERE tenant_id = ? AND deleted_at IS NULL
 ORDER BY created_at ASC`,
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

// insertFleet은 INSERT 쿼리를 실행합니다.
func insertFleet(ctx context.Context, tx storage.Tx, f robot.Fleet, policyJSON []byte) error {
	_, err := tx.Exec(ctx, `
INSERT INTO fleets (id, tenant_id, name, description, policy, created_at, updated_at, deleted_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`,
		f.ID, string(f.TenantID), f.Name, f.Description,
		string(policyJSON), f.CreatedAt.Format(rfc3339Nano), f.UpdatedAt.Format(rfc3339Nano))
	return err
}

// scanFleetRow는 *sql.Row를 Fleet으로 디코드합니다.
func scanFleetRow(row *sql.Row) (robot.Fleet, error) {
	var (
		id, tenantID, name, description, policy, createdAt, updatedAt string
		deletedAt                                                     sql.NullString
	)
	if err := row.Scan(&id, &tenantID, &name, &description, &policy, &createdAt, &updatedAt, &deletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return robot.Fleet{}, storage.ErrNotFound
		}
		return robot.Fleet{}, fmt.Errorf("robot: scan fleet: %w", err)
	}
	return assembleFleet(id, tenantID, name, description, policy, createdAt, updatedAt, deletedAt)
}

// scanFleetRowGeneric은 *sql.Rows를 Fleet으로 디코드합니다 (List에서 사용).
func scanFleetRowGeneric(rows *sql.Rows) (robot.Fleet, error) {
	var (
		id, tenantID, name, description, policy, createdAt, updatedAt string
		deletedAt                                                     sql.NullString
	)
	if err := rows.Scan(&id, &tenantID, &name, &description, &policy, &createdAt, &updatedAt, &deletedAt); err != nil {
		return robot.Fleet{}, fmt.Errorf("robot: scan fleet: %w", err)
	}
	return assembleFleet(id, tenantID, name, description, policy, createdAt, updatedAt, deletedAt)
}

func assembleFleet(id, tenantID, name, description, policy, createdAt, updatedAt string, deletedAt sql.NullString) (robot.Fleet, error) {
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
