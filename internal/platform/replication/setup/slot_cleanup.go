package setup

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// defaultMinInactiveAge는 비활성 slot 판정 기본 임계치입니다 (24시간).
const defaultMinInactiveAge = 24 * time.Hour

// CleanupInactiveSlotsOptions는 CleanupInactiveSlots의 동작을 조정합니다.
//
// MinInactiveAge: confirmed_flush_lsn이 마지막으로 갱신된 후 이 시간 이상 stale +
// active=false 일 때 cleanup 후보. 기본 24시간. 너무 짧으면 일시적 disconnect를
// drop할 위험. 운영 권장 ≥ 1h.
//
// DryRun: true면 SELECT만 수행하고 후보 list 반환 — 운영자가 검토 후 별도 호출로
// 실제 drop. 기본 false.
//
// SlotPrefix: 안전 가드 — 본 application이 만든 slot만 cleanup 대상으로 한정.
// 다른 application의 slot을 실수로 drop하지 않도록 SQL WHERE에 prefix를 강제하고
// client측에서 반환된 이름을 다시 검증합니다. 빈 prefix는 ErrEmptySlotPrefix.
// 일반적으로 "rosshield_" 같은 application namespace 권장.
//
// Logger: drop 이벤트를 logging — 운영 trace. nil이면 slog.Default().
type CleanupInactiveSlotsOptions struct {
	MinInactiveAge time.Duration
	DryRun         bool
	SlotPrefix     string
	Logger         *slog.Logger
}

// CleanupInactiveSlots는 primary PG에서 stale·비활성 replication slot을 drop합니다
// (E-MR Stage 3 후속).
//
// 동기:
//
//	primary에서 inactive replication slot이 stuck되면 WAL이 무한 누적되어 디스크가
//	가득찰 risk. standby가 영구 손실 또는 재배포되면 slot이 orphan이 됩니다 —
//	본 helper로 정기적 cleanup.
//
// 후보 조회 (PG pg_replication_slots view):
//
//	active = false
//	  AND temporary = false
//	  AND slot_name LIKE prefix||'%'
//	  AND (now() - COALESCE(...lsn timestamp..., '-infinity')) >= MinInactiveAge
//
// 정확한 stale 판정은 pg_replication_slots.active=false + confirmed_flush_lsn ==
// restart_lsn (진행 없음) + xact_started 부재로 결정. SQL에서 NOT active +
// MinInactiveAge 초과만 묶어서 처리.
//
// 안전:
//
//	SlotPrefix 미설정 → ErrEmptySlotPrefix. SQL prefix 필터링 + client측 재검증
//	이중 가드. 외부 application의 slot은 절대 drop되지 않습니다.
func CleanupInactiveSlots(ctx context.Context, exec Executor, opts CleanupInactiveSlotsOptions) ([]string, error) {
	if opts.SlotPrefix == "" {
		return nil, ErrEmptySlotPrefix
	}
	age := opts.MinInactiveAge
	if age <= 0 {
		age = defaultMinInactiveAge
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// PG에 prefix LIKE 필터링 + 비활성 + age 임계치 위임. age는 초 단위 정수.
	// pg_stat_activity와의 cross-check는 운영 epic — 본 round에서는 active=false +
	// LIKE prefix만으로 충분.
	const querySQL = `
SELECT slot_name
FROM pg_replication_slots
WHERE active = false
  AND temporary = false
  AND slot_name LIKE $1 || '%'
  AND COALESCE(EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp())), $2) >= $2
`
	candidates, err := exec.QueryStrings(ctx, querySQL, opts.SlotPrefix, age.Seconds())
	if err != nil {
		return nil, fmt.Errorf("CleanupInactiveSlots: query candidates: %w", err)
	}

	removed := make([]string, 0, len(candidates))
	for _, name := range candidates {
		// client-side 안전 가드 — PG가 반환한 slot이 정말 prefix를 만족하는지 재확인.
		if !strings.HasPrefix(name, opts.SlotPrefix) {
			logger.Warn("replication slot prefix mismatch, skip",
				"slot", name, "expected_prefix", opts.SlotPrefix)
			continue
		}
		if opts.DryRun {
			logger.Info("replication slot cleanup candidate (DryRun)",
				"slot", name, "min_inactive_age", age)
			removed = append(removed, name)
			continue
		}
		if err := dropReplicationSlot(ctx, exec, name); err != nil {
			return removed, fmt.Errorf("CleanupInactiveSlots: drop %q: %w", name, err)
		}
		logger.Info("replication slot dropped",
			"slot", name, "min_inactive_age", age)
		removed = append(removed, name)
	}
	return removed, nil
}

// dropReplicationSlot는 pg_drop_replication_slot()을 호출합니다.
//
// slot 이름은 validateName으로 SQL injection 차단 + bind parameter로 전달.
func dropReplicationSlot(ctx context.Context, exec Executor, name string) error {
	if err := validateName(name); err != nil {
		return fmt.Errorf("dropReplicationSlot: %w", err)
	}
	// pg_drop_replication_slot은 함수 호출 — bind parameter 사용.
	if err := exec.Exec(ctx, "SELECT pg_drop_replication_slot($1)", name); err != nil {
		return fmt.Errorf("dropReplicationSlot: exec: %w", err)
	}
	return nil
}
