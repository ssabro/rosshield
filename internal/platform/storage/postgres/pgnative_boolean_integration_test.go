//go:build integration

// pgnative_boolean_integration_test.go — E22-F R30-1.2 BOOLEAN 회수 검증.
//
// 마이그레이션 0031 적용 후 5 컬럼이 BOOLEAN 타입이며, Go bool BIND/SCAN이
// 양 driver(PG·SQLite)에서 그대로 동작함을 확인.
//
// 검증 패턴 (각 컬럼별):
//   - information_schema.columns 에서 data_type = 'boolean' 확인
//   - INSERT: Go bool 인자 → PG BOOLEAN 자동 캐스트
//   - SELECT: BOOLEAN 컬럼을 Go bool SCAN target으로 받기
//   - WHERE: parameterized bool 인자 (sqliterepo ListDueDeliveries 패턴)

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// TestIntegrationPGNativeBooleanColumnTypes — 5 컬럼이 BOOLEAN 타입인지 information_schema 검증.
func TestIntegrationPGNativeBooleanColumnTypes(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)
	ctx := context.Background()

	expected := []struct {
		table  string
		column string
	}{
		{"roles", "is_system"},
		{"compliance_profiles", "enabled"},
		{"webhook_endpoints", "enabled"},
		{"webhook_deliveries", "succeeded"},
		{"sso_providers", "enabled"},
	}

	for _, e := range expected {
		var dataType string
		err := store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
			return tx.QueryRow(ctx, `
SELECT data_type FROM information_schema.columns
WHERE table_name = ? AND column_name = ?`, e.table, e.column).Scan(&dataType)
		})
		if err != nil {
			t.Errorf("query %s.%s data_type: %v", e.table, e.column, err)
			continue
		}
		if !strings.EqualFold(dataType, "boolean") {
			t.Errorf("%s.%s: got data_type=%q, want boolean", e.table, e.column, dataType)
		}
	}
}

// TestIntegrationPGNativeBooleanRolesRoundTrip — roles.is_system BOOLEAN round-trip.
func TestIntegrationPGNativeBooleanRolesRoundTrip(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)
	ctx := context.Background()

	tenantID := storage.TenantID("tn_pgbool_role")
	now := time.Now().UTC().Format(time.RFC3339Nano)

	err := store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, ?, ?, ?)`,
			string(tenantID), "PG bool role", "trial", now)
		if e != nil {
			return e
		}
		// is_system=true (시스템 역할)
		_, e = tx.Exec(ctx, `
INSERT INTO roles (id, tenant_id, name, permissions, is_system, created_at)
VALUES (?, ?, ?, ?, ?, ?)`,
			"role_sys_1", string(tenantID), "admin", "[]", true, now)
		if e != nil {
			return e
		}
		// is_system=false (custom 역할)
		_, e = tx.Exec(ctx, `
INSERT INTO roles (id, tenant_id, name, permissions, is_system, created_at)
VALUES (?, ?, ?, ?, ?, ?)`,
			"role_cust_1", string(tenantID), "custom", "[]", false, now)
		return e
	})
	if err != nil {
		t.Fatalf("INSERT roles with bool: %v", err)
	}

	// SELECT bool 양방향
	var sys, cust bool
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		if e := tx.QueryRow(ctx, `SELECT is_system FROM roles WHERE id = ?`, "role_sys_1").Scan(&sys); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `SELECT is_system FROM roles WHERE id = ?`, "role_cust_1").Scan(&cust)
	})
	if err != nil {
		t.Fatalf("SELECT is_system as bool: %v", err)
	}
	if !sys {
		t.Errorf("role_sys_1 is_system: got false, want true")
	}
	if cust {
		t.Errorf("role_cust_1 is_system: got true, want false")
	}
}

// TestIntegrationPGNativeBooleanComplianceRoundTrip — compliance_profiles.enabled BOOLEAN round-trip.
func TestIntegrationPGNativeBooleanComplianceRoundTrip(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)
	ctx := context.Background()

	tenantID := storage.TenantID("tn_pgbool_comp")
	now := time.Now().UTC().Format(time.RFC3339Nano)

	err := store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, ?, ?, ?)`,
			string(tenantID), "PG bool comp", "trial", now)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `
INSERT INTO compliance_profiles (id, tenant_id, framework, framework_version, enabled, customizations_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"cp_1", string(tenantID), "isms-p", "2.0", true, "[]", now, now)
		return e
	})
	if err != nil {
		t.Fatalf("INSERT compliance_profiles with bool: %v", err)
	}

	var enabled bool
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		return tx.QueryRow(ctx, `SELECT enabled FROM compliance_profiles WHERE id = ?`, "cp_1").Scan(&enabled)
	})
	if err != nil {
		t.Fatalf("SELECT enabled as bool: %v", err)
	}
	if !enabled {
		t.Errorf("cp_1 enabled: got false, want true")
	}
}

// TestIntegrationPGNativeBooleanWebhookEndpointRoundTrip — webhook_endpoints.enabled BOOLEAN round-trip.
func TestIntegrationPGNativeBooleanWebhookEndpointRoundTrip(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)
	ctx := context.Background()

	tenantID := storage.TenantID("tn_pgbool_whep")
	now := time.Now().UTC().Format(time.RFC3339Nano)

	err := store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, ?, ?, ?)`,
			string(tenantID), "PG bool whep", "trial", now)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `
INSERT INTO webhook_endpoints (id, tenant_id, url, secret, events, format, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"wh_1", string(tenantID), "https://example.test", "secret", "[]", "json", false, now, now)
		return e
	})
	if err != nil {
		t.Fatalf("INSERT webhook_endpoints with bool: %v", err)
	}

	var enabled bool
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		return tx.QueryRow(ctx, `SELECT enabled FROM webhook_endpoints WHERE id = ?`, "wh_1").Scan(&enabled)
	})
	if err != nil {
		t.Fatalf("SELECT enabled as bool: %v", err)
	}
	if enabled {
		t.Errorf("wh_1 enabled: got true, want false")
	}
}

// TestIntegrationPGNativeBooleanWebhookDeliveryWhere — webhook_deliveries.succeeded BOOLEAN
// + WHERE 절 parameterized bool 인자 (ListDueDeliveries 패턴).
func TestIntegrationPGNativeBooleanWebhookDeliveryWhere(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)
	ctx := context.Background()

	tenantID := storage.TenantID("tn_pgbool_whd")
	now := time.Now().UTC().Format(time.RFC3339Nano)

	err := store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, ?, ?, ?)`,
			string(tenantID), "PG bool whd", "trial", now)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `
INSERT INTO webhook_endpoints (id, tenant_id, url, secret, events, format, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"wh_due_1", string(tenantID), "https://example.test", "secret", "[]", "json", true, now, now)
		if e != nil {
			return e
		}
		// pending delivery (succeeded=false)
		_, e = tx.Exec(ctx, `
INSERT INTO webhook_deliveries (id, endpoint_id, tenant_id, event_type, event_id, payload,
    attempt_count, last_attempted_at, next_attempt_at, succeeded, last_response_status, last_error, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, 0, '', ?)`,
			"whd_pending", "wh_due_1", string(tenantID), "scan.completed", "evt_1", []byte{},
			0, now, false, now)
		if e != nil {
			return e
		}
		// succeeded delivery (succeeded=true)
		_, e = tx.Exec(ctx, `
INSERT INTO webhook_deliveries (id, endpoint_id, tenant_id, event_type, event_id, payload,
    attempt_count, last_attempted_at, next_attempt_at, succeeded, last_response_status, last_error, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 200, '', ?)`,
			"whd_done", "wh_due_1", string(tenantID), "scan.completed", "evt_2", []byte{},
			1, now, now, true, now)
		return e
	})
	if err != nil {
		t.Fatalf("seed webhook_deliveries with bool: %v", err)
	}

	// WHERE succeeded = ? (parameterized false) — ListDueDeliveries 패턴
	var pendingCount int
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		return tx.QueryRow(ctx, `SELECT COUNT(*) FROM webhook_deliveries WHERE succeeded = ?`, false).Scan(&pendingCount)
	})
	if err != nil {
		t.Fatalf("COUNT pending with bool WHERE: %v", err)
	}
	if pendingCount != 1 {
		t.Errorf("pending count: got %d, want 1", pendingCount)
	}

	// UPDATE succeeded = ? (parameterized true) — MarkDeliverySucceeded 패턴
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE webhook_deliveries SET succeeded = ? WHERE id = ?`, true, "whd_pending")
		return e
	})
	if err != nil {
		t.Fatalf("UPDATE succeeded with bool: %v", err)
	}

	// 재검증: 이제 pending=0
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		return tx.QueryRow(ctx, `SELECT COUNT(*) FROM webhook_deliveries WHERE succeeded = ?`, false).Scan(&pendingCount)
	})
	if err != nil {
		t.Fatalf("COUNT pending after update: %v", err)
	}
	if pendingCount != 0 {
		t.Errorf("pending count after update: got %d, want 0", pendingCount)
	}
}

// TestIntegrationPGNativeBooleanSSORoundTrip — sso_providers.enabled BOOLEAN round-trip.
func TestIntegrationPGNativeBooleanSSORoundTrip(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)
	ctx := context.Background()

	tenantID := storage.TenantID("tn_pgbool_sso")
	now := time.Now().UTC().Format(time.RFC3339Nano)

	err := store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, ?, ?, ?)`,
			string(tenantID), "PG bool sso", "trial", now)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `
INSERT INTO sso_providers (id, tenant_id, type, name, enabled, config_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"ssop_1", string(tenantID), "oidc", "Provider A", true, "{}", now, now)
		return e
	})
	if err != nil {
		t.Fatalf("INSERT sso_providers with bool: %v", err)
	}

	var enabled bool
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		return tx.QueryRow(ctx, `SELECT enabled FROM sso_providers WHERE id = ?`, "ssop_1").Scan(&enabled)
	})
	if err != nil {
		t.Fatalf("SELECT enabled as bool: %v", err)
	}
	if !enabled {
		t.Errorf("ssop_1 enabled: got false, want true")
	}
}
