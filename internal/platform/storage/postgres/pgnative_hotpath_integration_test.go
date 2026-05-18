//go:build integration

// pgnative_hotpath_integration_test.go — E22-F R30-1 = C 하이브리드 (핫 path PG-native)
// 검증.
//
// sqliterepo가 PG-native 컬럼(TIMESTAMPTZ·JSONB)에서 driver-agnostic 코드 그대로
// 동작하는지 testcontainers PG 16에서 확인합니다. 0024 마이그레이션 후의 타입
// 호환성이 깨지면 본 테스트가 실패 — sqliterepo 어댑터 패치 또는 0024 롤백 신호.
//
// 검증 패턴:
//   - INSERT: 도메인 코드처럼 RFC3339 string 인자 → PG가 TIMESTAMPTZ로 자동 캐스트
//   - INSERT: JSON string 인자 → PG가 JSONB로 자동 캐스트
//   - SELECT: TIMESTAMPTZ 컬럼을 string SCAN target으로 받기
//   - SELECT: JSONB 컬럼을 string SCAN target으로 받기

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// TestIntegrationPGNativeAuditOccurredAtRoundTrip — audit_entries.occurred_at TIMESTAMPTZ 컬럼이
// RFC3339 string INSERT + string SCAN을 그대로 지원하는지.
//
// 0024 마이그레이션 후 sqliterepo의 audit Append 패턴(Format(RFC3339Nano))이 깨지지 않음을 보장.
func TestIntegrationPGNativeAuditOccurredAtRoundTrip(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)
	ctx := context.Background()

	tenantID := storage.TenantID("tn_pghotpath_aud")
	occurred := time.Date(2026, 5, 11, 12, 34, 56, 789012345, time.UTC)
	occurredStr := occurred.UTC().Format(time.RFC3339Nano)

	// 1. tenant 시드 (FK 충족)
	err := store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, ?, ?, ?)`,
			string(tenantID), "PG hotpath audit", "trial", occurredStr)
		return e
	})
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// 2. audit_entries INSERT (sqliterepo 패턴 정확히 모방 — Stage 2 leader_epoch 컬럼 포함)
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		zeroHash := make([]byte, 32)
		payloadDigest := make([]byte, 32)
		hash := make([]byte, 32)
		hash[0] = 0xab
		_, e := tx.Exec(ctx, `
INSERT INTO audit_entries (
    tenant_id, seq, occurred_at,
    actor_type, actor_id, actor_ip, actor_ua,
    action, target_type, target_id,
    payload_digest, outcome, error_code, error_message,
    prev_hash, hash, leader_epoch
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(tenantID), int64(1), occurredStr,
			"system", "test", nil, nil,
			"hotpath.insert", "tenant", string(tenantID),
			payloadDigest, "success", nil, nil,
			zeroHash, hash, nil)
		return e
	})
	if err != nil {
		t.Fatalf("INSERT audit_entries with TIMESTAMPTZ from RFC3339 string: %v", err)
	}

	// 3. SELECT — string SCAN target. PG TIMESTAMPTZ → string은 RFC3339-like 표현 (정확한 포맷은 driver 의존).
	var got string
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		return tx.QueryRow(ctx, `SELECT occurred_at FROM audit_entries WHERE tenant_id = ? AND seq = ?`,
			string(tenantID), int64(1)).Scan(&got)
	})
	if err != nil {
		t.Fatalf("SELECT occurred_at as string: %v", err)
	}
	if got == "" {
		t.Errorf("SELECT returned empty string")
	}
	// PG가 timestamp를 RFC3339-like 표현으로 직렬화. 정확한 포맷은 검증 X (driver 의존),
	// 다만 round-trip이 동작하는 것 + 빈 string이 아닌 것을 핵심으로 봄.
	t.Logf("INSERT %s → SELECT %s", occurredStr, got)
}

// TestIntegrationPGNativeAuditChainHeadUpdatedAt — audit_chain_heads.updated_at TIMESTAMPTZ
// 동일 round-trip.
func TestIntegrationPGNativeAuditChainHeadUpdatedAt(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)
	ctx := context.Background()

	tenantID := storage.TenantID("tn_pghotpath_head")
	updated := time.Now().UTC().Format(time.RFC3339Nano)

	err := store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, ?, ?, ?)`,
			string(tenantID), "PG hotpath head", "trial", updated)
		if e != nil {
			return e
		}
		hash := make([]byte, 32)
		hash[0] = 0xcd
		_, e = tx.Exec(ctx, `INSERT INTO audit_chain_heads (tenant_id, seq, hash, updated_at) VALUES (?, ?, ?, ?)`,
			string(tenantID), int64(0), hash, updated)
		return e
	})
	if err != nil {
		t.Fatalf("INSERT audit_chain_heads: %v", err)
	}

	var got string
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		return tx.QueryRow(ctx, `SELECT updated_at FROM audit_chain_heads WHERE tenant_id = ?`,
			string(tenantID)).Scan(&got)
	})
	if err != nil {
		t.Fatalf("SELECT updated_at as string: %v", err)
	}
	if got == "" {
		t.Errorf("SELECT updated_at returned empty string")
	}
}

// TestIntegrationPGNativeInsightsEvidenceJSON — insights.evidence_json JSONB 컬럼이
// JSON string INSERT + string SCAN을 지원하는지. GIN 인덱스 효과는 별도 query plan 검증
// (본 테스트는 호환성만).
func TestIntegrationPGNativeInsightsEvidenceJSON(t *testing.T) {
	t.Parallel()
	store, _ := newPGFixture(t)
	ctx := context.Background()

	tenantID := storage.TenantID("tn_pghotpath_ins")
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// tenant + 핵심 dependency 시드
	err := store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, ?, ?, ?)`,
			string(tenantID), "PG hotpath insight", "trial", now)
		return e
	})
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// insight INSERT — evidence_json은 JSON 문자열로 전달, PG가 JSONB로 자동 파싱
	insightID := "in_pghotpath_001"
	evidenceJSON := `[{"refType":"file","path":"/etc/ros2.yaml"},{"refType":"output","cmd":"ros2 doctor"}]`
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `
INSERT INTO insights (
    id, tenant_id, kind, severity, summary,
    evidence_json,
    created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			insightID, string(tenantID), "drift", "high", "summary",
			evidenceJSON,
			now)
		return e
	})
	if err != nil {
		t.Fatalf("INSERT insights with JSON string into JSONB: %v", err)
	}

	// SELECT — string SCAN. PG JSONB → string은 정규화된 JSON.
	var got string
	err = store.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		return tx.QueryRow(ctx, `SELECT evidence_json FROM insights WHERE id = ?`, insightID).Scan(&got)
	})
	if err != nil {
		t.Fatalf("SELECT evidence_json as string: %v", err)
	}
	// JSONB normalization — 공백 제거·키 정렬 등이 일어날 수 있음. 키 존재만 확인.
	for _, want := range []string{"refType", "file", "/etc/ros2.yaml", "ros2 doctor"} {
		if !strings.Contains(got, want) {
			t.Errorf("SELECT evidence_json missing %q: got=%s", want, got)
		}
	}
}
