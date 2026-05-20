// Package replicationcleanupjob은 PG replication slot cleanup cron job 어댑터입니다
// (E-MR Stage 3 후속, v0.6.9 carryover).
//
// 책임:
//   - 정기 cron tick 시 primary PG에서 stale·비활성 replication slot을 detect + drop
//     (setup.CleanupInactiveSlots wrap).
//   - HA 활성 시 cronsched RoleProvider gate(E25 Stage 4a)가 follower tick을 silent
//     skip하므로 leader 단일 인스턴스만 cleanup 수행.
//
// 설계:
//   - in-process cron (cronsched / robfig) 사용 — P3 에어갭 (외부 cron 의존 0).
//   - SlotPrefix는 운영자 명시 필수 — 빈 prefix는 register error로 fail-fast (다른
//     application slot 실수 drop 방지).
//   - replication setup 패키지 본체는 변경 없음 — 본 어댑터는 기존 공개 API만 호출.
//
// 본 패키지가 다루지 않는 carryover (별 epic):
//   - publication tables 변경 자동 sync는 ensurePublication exists 경로에서 처리됨
//     (별 cron 불필요).
//   - failover 자동화 + DNS hook은 Phase 8+ 영역.
package replicationcleanupjob

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/platform/replication/setup"
	"github.com/ssabro/rosshield/internal/platform/scheduler"
)

// DefaultJobID는 cron 등록 시 사용되는 기본 식별자입니다.
const DefaultJobID = "system.replication.slot.cleanup"

// Deps는 Register 의존성입니다. Executor·SlotPrefix·Logger는 필수.
type Deps struct {
	// Executor는 primary PG에 붙는 setup.Executor 인터페이스 구현입니다.
	// bootstrap에서 setup.PgxExecutor(pgxpool) wrap하여 주입.
	Executor setup.Executor
	// SlotPrefix는 안전 가드 — 본 application이 만든 slot만 cleanup 대상.
	// 빈 문자열은 Register에서 error (다른 application slot 실수 drop 방지).
	SlotPrefix string
	// MinInactiveAge는 slot이 비활성 상태로 머문 최소 시간 — 이 미만은 cleanup 안 함.
	// 0이면 setup 패키지 기본(24h) 사용.
	MinInactiveAge time.Duration
	// DryRun=true면 후보만 logging하고 실제 drop 안 함 (운영자 검토용).
	DryRun bool
	// Logger는 drop 이벤트 logging. nil이면 slog.Default().
	Logger *slog.Logger
}

// Register는 Scheduler에 정기 slot cleanup job을 등록합니다.
//
// spec=""이면 no-op (자동 cleanup 비활성 — manual 호출만). SlotPrefix가 빈 값이면
// register error로 fail-fast — bootstrap에서 부팅 자체를 막아 안전 잠금.
//
// HA 활성 시 cronsched가 follower tick을 silent skip하므로 leader 단일 인스턴스만 수행.
func Register(sch scheduler.Scheduler, deps Deps, jobID, spec string) error {
	if sch == nil {
		return fmt.Errorf("replicationcleanupjob: Scheduler required")
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil // 자동 cleanup 비활성.
	}
	if deps.Executor == nil {
		return fmt.Errorf("replicationcleanupjob: Executor required")
	}
	if strings.TrimSpace(deps.SlotPrefix) == "" {
		return fmt.Errorf("replicationcleanupjob: SlotPrefix required (safety guard — 다른 application slot 실수 drop 방지)")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if strings.TrimSpace(jobID) == "" {
		jobID = DefaultJobID
	}

	job := func(ctx context.Context) error {
		return RunOnce(ctx, deps)
	}
	if err := sch.Schedule(jobID, spec, job); err != nil {
		return fmt.Errorf("replicationcleanupjob: schedule %q: %w", spec, err)
	}
	deps.Logger.Info("replication slot cleanup auto-schedule registered",
		"spec", spec, "jobId", jobID, "slotPrefix", deps.SlotPrefix,
		"minInactiveAge", deps.MinInactiveAge, "dryRun", deps.DryRun)
	return nil
}

// RunOnce는 한 번의 cleanup 라운드를 수행합니다.
//
// 본 함수는 cron job 본문이면서 동시에 테스트가 직접 invoke할 수 있는 entry point입니다.
// cleanup 성공·실패 모두 결과 카운트를 logging.
func RunOnce(ctx context.Context, deps Deps) error {
	if deps.Executor == nil {
		return fmt.Errorf("replicationcleanupjob: RunOnce: Executor required")
	}
	if strings.TrimSpace(deps.SlotPrefix) == "" {
		return fmt.Errorf("replicationcleanupjob: RunOnce: SlotPrefix required")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	removed, err := setup.CleanupInactiveSlots(ctx, deps.Executor, setup.CleanupInactiveSlotsOptions{
		MinInactiveAge: deps.MinInactiveAge,
		DryRun:         deps.DryRun,
		SlotPrefix:     deps.SlotPrefix,
		Logger:         deps.Logger,
	})
	if err != nil {
		return fmt.Errorf("replicationcleanupjob: RunOnce: %w", err)
	}
	deps.Logger.Info("replication slot cleanup tick complete",
		"removed", len(removed), "dryRun", deps.DryRun, "slotPrefix", deps.SlotPrefix)
	return nil
}
