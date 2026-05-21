// Package keyrotationjob 은 audit chain signer key rotation cron job 어댑터입니다
// (Phase 10.D-3).
//
// 책임:
//   - 정기 cron tick 시 keyrotation.KeyRotator.RotateNow 호출.
//   - HA 활성 시 cronsched RoleProvider gate(E25 Stage 4a) 가 follower tick 을 silent skip
//     — 본 어댑터는 leader-only enforce 를 직접 수행하지 않음 (KeyRotator 내부에서도 추가
//     gate 가 동작 — defense-in-depth).
//   - schedule="" → no-op (자동 rotation 비활성, manual API only — D-P10D-1 옵션 C 의 emergency
//     override path 와 동등).
//   - rotation 결과를 INFO/WARN 로그로 emit. ErrTooSoon · ErrNotLeader 는 DEBUG (정상 skip).
//
// 결정:
//   - in-process cron (cronsched / robfig) 사용 — P3 에어갭 (외부 cron 의존 0).
//   - scheduler 패키지가 audit/keyrotation 을 import 하면 cycle 가능성 — 어댑터를 별 sub-package
//     로 분리 (entry-segment rotationjob 일관 패턴).
package keyrotationjob

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ssabro/rosshield/internal/domain/audit/keyrotation"
	"github.com/ssabro/rosshield/internal/platform/scheduler"
)

// DefaultJobID 는 cron 등록 시 사용되는 기본 식별자입니다.
const DefaultJobID = "system.audit.keyrotation.auto"

// DefaultQuarterlySpec 은 D-P10D-2 quarterly default 의 robfig/cron spec 입니다.
//
// 90 일 baseline — "0 0 1 */3 *" (매 분기 1 일 00:00) 보다는 운영 부담 평탄화를 위해
// "@every 2160h" (90 일 간격) 채택. customer config 로 override 가능.
const DefaultQuarterlySpec = "@every 2160h"

// Register 는 Scheduler 에 정기 audit chain key rotation job 을 등록합니다.
//
// spec="" → no-op (자동 rotation 비활성, manual API only).
// rotator nil → error (구성 누락).
// HA 활성 시 cronsched 가 follower tick 을 silent skip — KeyRotator 내부에서도 leader gate
// (defense-in-depth).
func Register(sch scheduler.Scheduler, rotator *keyrotation.KeyRotator, logger *slog.Logger, jobID, spec string) error {
	if sch == nil {
		return fmt.Errorf("keyrotationjob: Scheduler required")
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	if rotator == nil {
		return fmt.Errorf("keyrotationjob: KeyRotator required")
	}
	if logger == nil {
		return fmt.Errorf("keyrotationjob: Logger required")
	}
	if strings.TrimSpace(jobID) == "" {
		jobID = DefaultJobID
	}

	job := func(ctx context.Context) error {
		err := rotator.RotateNow(ctx, keyrotation.TriggerScheduler)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, keyrotation.ErrNotLeader):
			logger.Debug("audit key rotation skipped (follower)")
			return nil
		case errors.Is(err, keyrotation.ErrTooSoon):
			logger.Debug("audit key rotation skipped (min interval)")
			return nil
		default:
			logger.Error("audit key rotation failed", "err", err.Error())
			return err
		}
	}
	if err := sch.Schedule(jobID, spec, job); err != nil {
		return fmt.Errorf("keyrotationjob: schedule %q: %w", spec, err)
	}
	logger.Info("audit chain key rotation auto-schedule registered",
		"spec", spec, "jobId", jobID)
	return nil
}
