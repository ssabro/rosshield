// Package sqliterepo는 fleet.Service의 SQLite 어댑터입니다.
//
// read-only — fleets 테이블에서 tenant scope 살아있는 row만 name ASC로 반환.
package sqliterepo

import (
	"context"
	"fmt"
	"time"

	"github.com/ssabro/rosshield/internal/domain/fleet"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// rfc3339Nano는 DB 시간 칼럼 직렬화 포맷입니다.
const rfc3339Nano = time.RFC3339Nano

// Repo는 fleet.Service의 SQLite 구현입니다.
type Repo struct{}

// New는 새 Repo를 반환합니다.
func New() *Repo {
	return &Repo{}
}

// ListFleets는 tenant 내 살아있는 fleets를 name ASC로 반환합니다 (deleted_at IS NULL).
func (r *Repo) ListFleets(ctx context.Context, tx storage.Tx) ([]fleet.Fleet, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	rows, err := tx.Query(ctx, `
SELECT id, tenant_id, name, description, created_at, updated_at
  FROM fleets
 WHERE tenant_id = ? AND deleted_at IS NULL
 ORDER BY name ASC`, string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("fleet: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []fleet.Fleet
	for rows.Next() {
		var (
			id, tid, name, desc    string
			createdStr, updatedStr string
		)
		if err := rows.Scan(&id, &tid, &name, &desc, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("fleet: scan: %w", err)
		}
		f := fleet.Fleet{
			ID:          id,
			TenantID:    storage.TenantID(tid),
			Name:        name,
			Description: desc,
		}
		if t, err := time.Parse(rfc3339Nano, createdStr); err == nil {
			f.CreatedAt = t
		}
		if t, err := time.Parse(rfc3339Nano, updatedStr); err == nil {
			f.UpdatedAt = t
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fleet: rows: %w", err)
	}
	return out, nil
}
