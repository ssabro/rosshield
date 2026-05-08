package main

// license_usage_adapter.go — E24-D: license.UsageReader → 도메인 데이터 결선.
//
// 설계:
//   - license platform은 도메인을 import 안 함 (P5). 어댑터를 cmd/* 에 두어
//     storage.Storage 직접 SQL로 사용량을 집계.
//   - bootstrap이 본 어댑터를 만들어 license.NewEnforcer에 주입.
//   - Tx는 호출 시점마다 새로 — quota check는 handler 진입 직후라 별도 트랜잭션 OK.
//   - 모든 쿼리는 tenant_id 필터 (멀티테넌시 격리).
//
// 메서드 책임:
//   - CurrentRobots:   activé robot 수 (deleted_at IS NULL)
//   - ScansToday:      오늘 UTC 자정 이후 생성된 scan_session 수
//   - LLMTokensToday:  오늘 UTC 자정 이후 생성된 advisor_turns 의 input+output 토큰 합

import (
	"context"
	"fmt"
	"time"

	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// licenseUsageAdapter는 license.UsageReader 구현체입니다.
//
// storage.Storage를 직접 사용 — 도메인 service의 ListXxx 표면 추가는 회피
// (단순 COUNT/SUM 집계는 quota-only 표면이라 도메인 인터페이스 오염 회피).
type licenseUsageAdapter struct {
	storage storage.Storage
	clock   clock.Clock
}

// newLicenseUsageAdapter는 본 어댑터를 만듭니다.
func newLicenseUsageAdapter(s storage.Storage, c clock.Clock) *licenseUsageAdapter {
	return &licenseUsageAdapter{storage: s, clock: c}
}

// CurrentRobots는 tenant의 활성 robot 수를 반환합니다 (soft-deleted 제외).
func (a *licenseUsageAdapter) CurrentRobots(ctx context.Context, tenantID string) (int, error) {
	if tenantID == "" {
		return 0, fmt.Errorf("license usage: tenantID is required")
	}
	var count int
	err := a.storage.Tx(
		storage.WithTenantID(ctx, storage.TenantID(tenantID)),
		func(ctx context.Context, tx storage.Tx) error {
			row := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM robots WHERE tenant_id = ? AND deleted_at IS NULL`,
				tenantID,
			)
			return row.Scan(&count)
		})
	if err != nil {
		return 0, fmt.Errorf("license usage: count robots: %w", err)
	}
	return count, nil
}

// ScansToday는 오늘 (UTC 자정 이후) 시작된 scan session 수를 반환합니다.
//
// created_at 기준 — pending 단계여도 카운트 (사용자가 quota 소진 후 새 scan 시작 차단 의도).
func (a *licenseUsageAdapter) ScansToday(ctx context.Context, tenantID string) (int, error) {
	if tenantID == "" {
		return 0, fmt.Errorf("license usage: tenantID is required")
	}
	startOfDay := todayStartUTC(a.clock.Now())
	var count int
	err := a.storage.Tx(
		storage.WithTenantID(ctx, storage.TenantID(tenantID)),
		func(ctx context.Context, tx storage.Tx) error {
			row := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM scan_sessions WHERE tenant_id = ? AND created_at >= ?`,
				tenantID, startOfDay.Format(time.RFC3339Nano),
			)
			return row.Scan(&count)
		})
	if err != nil {
		return 0, fmt.Errorf("license usage: count scans today: %w", err)
	}
	return count, nil
}

// LLMTokensToday는 오늘 (UTC 자정 이후) advisor_turns 의 input+output 토큰 합을 반환합니다.
//
// advisor_turns에만 의존 — Phase 3 시점 LLM 토큰 사용은 advisor 도메인이 유일.
// 후속 stage에서 다른 LLM 호출처(compliance suggester 등)가 별 trace 테이블을 가지면
// 본 메서드가 UNION 합산하도록 확장.
func (a *licenseUsageAdapter) LLMTokensToday(ctx context.Context, tenantID string) (int, error) {
	if tenantID == "" {
		return 0, fmt.Errorf("license usage: tenantID is required")
	}
	startOfDay := todayStartUTC(a.clock.Now())
	var sum int
	err := a.storage.Tx(
		storage.WithTenantID(ctx, storage.TenantID(tenantID)),
		func(ctx context.Context, tx storage.Tx) error {
			row := tx.QueryRow(ctx,
				`SELECT COALESCE(SUM(input_tokens + output_tokens), 0)
				   FROM advisor_turns
				  WHERE tenant_id = ? AND created_at >= ?`,
				tenantID, startOfDay.Format(time.RFC3339Nano),
			)
			return row.Scan(&sum)
		})
	if err != nil {
		return 0, fmt.Errorf("license usage: sum llm tokens today: %w", err)
	}
	return sum, nil
}

// todayStartUTC는 주어진 시점이 속한 UTC 날짜의 자정(00:00:00.000000000)을 반환합니다.
//
// quota는 UTC 기준 일자로 reset (기준 시간대 정의 — 운영자 혼선 방지).
func todayStartUTC(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
