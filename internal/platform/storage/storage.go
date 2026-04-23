// Package storage는 도메인 영속화 진입점을 제공합니다.
// 인터페이스는 드라이버 무지(driver-agnostic). SQLite·PostgreSQL 어댑터가 sub-package로 구현합니다.
//
// 트랜잭션 진입점은 두 가지로 분리됩니다 (R1-2 결정):
//   - Tx(ctx, fn): tenant-scoped. ctx에 TenantID 없으면 ErrTenantMissing.
//   - Bootstrap(ctx, fn): tenant-less. 마이그레이션·system seed 전용.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type TenantID string

type ctxKey int

const tenantKey ctxKey = iota + 1

// WithTenantID는 ctx에 TenantID를 주입합니다. HTTP 미들웨어가 JWT 검증 후 호출.
func WithTenantID(ctx context.Context, id TenantID) context.Context {
	return context.WithValue(ctx, tenantKey, id)
}

// TenantIDFromContext는 ctx에서 TenantID를 추출합니다. 없으면 빈 문자열.
func TenantIDFromContext(ctx context.Context) TenantID {
	v, _ := ctx.Value(tenantKey).(TenantID)
	return v
}

type Config struct {
	Driver  string        // "sqlite"
	DSN     string        // 파일 경로 또는 postgres:// URL
	MaxOpen int           // SQLite 권장 1, PG 권장 25
	BusyMS  int           // SQLite busy_timeout (ms), 기본 5000
	LogSlow time.Duration // 느린 쿼리 임계 (예: 200ms). 0이면 비활성.
}

// Storage는 트랜잭션 진입점입니다. 도메인 코드는 이것만 주입받습니다.
type Storage interface {
	// Tx는 tenant-scoped 트랜잭션. ctx에 TenantID 없으면 ErrTenantMissing.
	// fn 반환값이 nil이면 commit, error면 rollback. panic 시 recover → rollback → re-panic.
	Tx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error

	// Bootstrap은 tenant-less 트랜잭션. 마이그레이션·system seed 전용 진입점.
	// 일반 도메인 코드에서 호출 금지(린트로 차단 예정).
	Bootstrap(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error

	// Migrate는 부팅 경로에서만 호출. 실패 시 caller는 fail-fast.
	// (T4 stub: 항상 nil 반환. T5에서 goose 통합.)
	Migrate(ctx context.Context) error

	Close() error
}

// Tx는 트랜잭션 안에서만 유효한 쿼리 핸들. *sql.Tx를 노출하지 않습니다.
type Tx interface {
	Exec(ctx context.Context, query string, args ...any) (sql.Result, error)
	Query(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) *sql.Row

	// TenantID는 §7 테넌시 격리용. Tx() 진입점은 항상 채워짐, Bootstrap()은 빈 값.
	TenantID() TenantID
}

// 공통 에러 — 드라이버별 에러를 도메인이 알 필요 없도록 추상화.
var (
	ErrNotFound        = errors.New("storage: not found")
	ErrConflict        = errors.New("storage: conflict")
	ErrForeignKey      = errors.New("storage: foreign key violation")
	ErrImmutable       = errors.New("storage: target is immutable")
	ErrTenantMissing   = errors.New("storage: tenant context missing")
	ErrMigrationLocked = errors.New("storage: migration already in progress")
)
