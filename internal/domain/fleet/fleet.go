// Package fleet은 Fleet 도메인 read 모델 + Service interface입니다.
//
// Phase 1 범위에서 fleets는 robot 그룹 컨테이너 — 별 mutation 도메인은 시드/마이그레이션만 사용.
// 본 패키지는 read-only 표면(ListFleets)만 제공. mutation은 후속 epic.
package fleet

import (
	"context"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Fleet은 fleets 테이블의 read 모델입니다.
type Fleet struct {
	ID          string
	TenantID    storage.TenantID
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Service는 Fleet read 표면입니다.
type Service interface {
	// ListFleets는 tenant 내 살아있는 fleets를 name ASC로 반환합니다 (deleted_at IS NULL).
	ListFleets(ctx context.Context, tx storage.Tx) ([]Fleet, error)
}

// 도메인 에러.
var (
	ErrFleetNotFound = errors.New("fleet: not found")
)
