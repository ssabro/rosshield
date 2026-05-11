package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// seedSystemTenant은 cross-tenant 공유 자산(builtin pack 등)의 소속 tenant row를
// idempotent INSERT 합니다 (E12 Stage 8).
//
// `tenants(id='system')`이 없으면 packs FK 위반으로 seedBuiltinPacks가 silent fail.
// 본 함수는 driver-aware UPSERT로 sqlite/postgres 둘 다 호환.
//
// 비-fatal: 에러도 server boot 막지 않음 (degraded mode, 운영자가 별도 처리 가능).
func seedSystemTenant(ctx context.Context, store storage.Storage, driver string,
	tenantID storage.TenantID) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := string(tenantID)

	var query string
	switch driver {
	case "postgres":
		query = `INSERT INTO tenants (id, name, plan, created_at) VALUES ($1, 'System', 'desktop_free', $2) ON CONFLICT (id) DO NOTHING`
	default: // sqlite
		query = `INSERT OR IGNORE INTO tenants (id, name, plan, created_at) VALUES (?, 'System', 'desktop_free', ?)`
	}

	return store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, query, id, now)
		if err != nil {
			return fmt.Errorf("seedSystemTenant: %w", err)
		}
		return nil
	})
}
