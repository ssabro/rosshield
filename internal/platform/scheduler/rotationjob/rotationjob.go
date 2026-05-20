// Package rotationjob은 audit chain rotation cron job 어댑터입니다 (E32 Stage 6).
//
// 책임:
//   - 정기 cron tick 시 TenantLister가 반환하는 모든 tenant에 대해 audit chain rotation을
//     trigger (manual API와 동일 결과).
//   - tenant마다 단일 Tx — 한 tenant rotation 실패가 다른 tenant에 전파되지 않음.
//
// 결정:
//   - in-process cron (cronsched / robfig) 사용 — P3 에어갭 (외부 cron 의존 0).
//   - HA 활성 시 cronsched RoleProvider gate(E25 Stage 4a)가 follower tick을 silent skip.
//   - segmentNumber·fromSeq·toSeq 자동 산출 (LatestSegmentNumber + audit.Service.Head + 직전
//     segment.LastEntryID + 1).
//   - 빈 체인 또는 새 entry 없음(toSeq < fromSeq) → silent skip (no error).
//   - rotation 패키지 본체는 변경 없음 — 본 어댑터는 기존 공개 API만 호출.
//   - scheduler 패키지가 audit/rotation을 import하면 cycle (audit/checkpoint.go가 scheduler
//     interface 참조)이므로 sub-package로 분리.
package rotationjob

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
	"github.com/ssabro/rosshield/internal/platform/scheduler"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// DefaultJobID는 cron 등록 시 사용되는 기본 식별자입니다.
const DefaultJobID = "system.audit.rotation.auto"

// TenantLister는 rotation job이 walk할 tenant 목록을 제공합니다.
//
// bootstrap에서 fleet_scan_scheduler.listAllTenantIDs와 같은 SELECT를 함수로 wrap해 주입.
type TenantLister interface {
	ListActiveTenants(ctx context.Context) ([]storage.TenantID, error)
}

// TenantListerFunc는 함수를 TenantLister로 어댑팅합니다 (bootstrap 일회용 클로저용).
type TenantListerFunc func(ctx context.Context) ([]storage.TenantID, error)

// ListActiveTenants는 TenantLister 인터페이스 구현입니다.
func (f TenantListerFunc) ListActiveTenants(ctx context.Context) ([]storage.TenantID, error) {
	return f(ctx)
}

// Deps는 Register 의존성입니다.
//
// Storage·Audit·Rotator·Tenants·Logger 모두 필수. nil이면 register 시 error.
type Deps struct {
	Storage storage.Storage
	Audit   audit.Service
	Rotator *rotation.Rotator
	Tenants TenantLister
	Logger  *slog.Logger
}

// Register는 Scheduler에 정기 audit chain rotation job을 등록합니다.
//
// spec=""이면 no-op (자동 rotation 비활성 — manual API only). 빈 체인 또는 신규 entry 없는
// tenant는 silent skip. 일부 tenant rotation 실패는 logger.Error로 기록하되 다른 tenant
// 처리를 막지 않음 (best-effort) — 단 모두 실패한 경우 job error 반환 (cronsched는 warn 로그).
//
// HA 활성 시 cronsched가 follower tick을 silent skip하므로 leader 단일 인스턴스만 rotation 수행.
func Register(sch scheduler.Scheduler, deps Deps, jobID, spec string) error {
	if sch == nil {
		return fmt.Errorf("rotationjob: Scheduler required")
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil // 자동 rotation 비활성 — manual API only.
	}
	if deps.Storage == nil || deps.Audit == nil || deps.Rotator == nil || deps.Tenants == nil || deps.Logger == nil {
		return fmt.Errorf("rotationjob: all deps required (Storage·Audit·Rotator·Tenants·Logger)")
	}
	if strings.TrimSpace(jobID) == "" {
		jobID = DefaultJobID
	}

	job := func(ctx context.Context) error {
		return RunOnce(ctx, deps)
	}
	if err := sch.Schedule(jobID, spec, job); err != nil {
		return fmt.Errorf("rotationjob: schedule %q: %w", spec, err)
	}
	deps.Logger.Info("audit rotation auto-schedule registered",
		"spec", spec, "jobId", jobID)
	return nil
}

// RunOnce는 등록된 모든 tenant에 대해 한 번씩 rotation을 시도합니다.
//
// best-effort: 일부 tenant rotation 실패는 logger.Error 후 다음 tenant 진행.
// 모두 실패 + 1개 이상 시도 → job error 반환. 빈 tenant 목록 = silent success.
//
// 본 함수는 cron job 본문이면서 동시에 테스트가 직접 invoke할 수 있는 entry point입니다.
func RunOnce(ctx context.Context, deps Deps) error {
	if deps.Storage == nil || deps.Audit == nil || deps.Rotator == nil || deps.Tenants == nil || deps.Logger == nil {
		return fmt.Errorf("rotationjob: RunOnce: all deps required")
	}

	tenantIDs, err := deps.Tenants.ListActiveTenants(ctx)
	if err != nil {
		return fmt.Errorf("rotationjob: list tenants: %w", err)
	}
	if len(tenantIDs) == 0 {
		deps.Logger.Debug("audit rotation: no active tenants, skipping")
		return nil
	}

	var (
		attempted int
		rotated   int
		skipped   int
		failed    []string
	)
	for _, tid := range tenantIDs {
		attempted++
		outcome, err := rotateOneTenant(ctx, deps, tid)
		switch {
		case err != nil:
			failed = append(failed, string(tid))
			deps.Logger.Error("audit rotation failed for tenant",
				"tenantId", string(tid), "err", err.Error())
		case outcome == outcomeSkipped:
			skipped++
			deps.Logger.Debug("audit rotation skipped (empty range)",
				"tenantId", string(tid))
		case outcome == outcomeRotated:
			rotated++
		}
	}

	deps.Logger.Info("audit rotation tick complete",
		"attempted", attempted, "rotated", rotated, "skipped", skipped, "failed", len(failed))

	// 일부 실패는 warn만 (다음 tick에서 재시도). 모두 실패 + skipped 0 → error.
	if len(failed) > 0 && rotated == 0 && skipped == 0 {
		return fmt.Errorf("rotationjob: all %d tenants failed: %v", len(failed), failed)
	}
	return nil
}

type outcome int

const (
	outcomeRotated outcome = iota
	outcomeSkipped
)

// rotateOneTenant는 단일 tenant scope Tx 안에서 rotation을 수행합니다.
//
//  1. audit.Service.Head → 현재 head seq (= toSeq 후보)
//  2. LatestSegmentNumber → 직전 segment 번호 (= N-1)
//  3. 직전 segment의 LastEntryID → fromSeq = LastEntryID + 1 (없으면 1)
//  4. toSeq < fromSeq → silent skip (rotation 대상 entry 없음)
//  5. Rotator.Rotate(ctx, tx, tenantID, N, fromSeq, toSeq)
func rotateOneTenant(ctx context.Context, deps Deps, tenantID storage.TenantID) (outcome, error) {
	tenantCtx := storage.WithTenantID(ctx, tenantID)

	result := outcomeSkipped
	err := deps.Storage.Tx(tenantCtx, func(c context.Context, tx storage.Tx) error {
		head, err := deps.Audit.Head(c, tx, tenantID)
		if err != nil {
			return fmt.Errorf("read head: %w", err)
		}
		if head.Seq == 0 {
			// 빈 체인 — rotation 의미 없음. silent skip.
			return nil
		}

		latestSeg, err := rotation.LatestSegmentNumber(c, tx, tenantID)
		if err != nil {
			return fmt.Errorf("latest segment: %w", err)
		}

		var fromSeq int64 = 1
		if latestSeg > 0 {
			prev, err := rotation.GetSegment(c, tx, tenantID, latestSeg)
			if err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					return fmt.Errorf("prev segment %d missing (chain gap)", latestSeg)
				}
				return fmt.Errorf("get prev segment %d: %w", latestSeg, err)
			}
			fromSeq = prev.LastEntryID + 1
		}

		toSeq := head.Seq
		if toSeq < fromSeq {
			// 직전 rotation 이후 새 entry 0 — skip.
			return nil
		}

		nextSeg := latestSeg + 1
		rec, err := deps.Rotator.Rotate(c, tx, tenantID, nextSeg, fromSeq, toSeq)
		if err != nil {
			return fmt.Errorf("rotate seg %d (seq %d..%d): %w", nextSeg, fromSeq, toSeq, err)
		}
		deps.Logger.Info("audit rotation completed",
			"tenantId", string(tenantID),
			"segmentNumber", rec.SegmentNumber,
			"firstEntryId", rec.FirstEntryID,
			"lastEntryId", rec.LastEntryID,
			"entryCount", rec.EntryCount,
			"archiveUri", rec.ArchiveURI)
		result = outcomeRotated
		return nil
	})
	if err != nil {
		return outcomeSkipped, err
	}
	return result, nil
}
