package main

// fleet_scan_scheduler.go — FleetPolicy.ScanSchedule cron 결선 (scheduler-driven scan).
//
// 책임:
//   - 부팅 시 모든 tenant의 모든 활성 fleet을 walk (RegisterAll)
//   - admin이 fleet을 등록·이름 변경·삭제하면 dynamic 재등록 (Reconcile / Cancel)
//   - cron tick 시: pre-check active session(있으면 silent skip) → robots × pack checks 사전 fetch →
//     scan.Service.StartScan → scanrun.Orchestrator.Run
//
// HA 결합: cronsched RoleProvider gate가 follower tick을 silent skip — leader 단일 인스턴스만 자동 scan.
// 동시성 가드: scan.ErrFleetActiveScanExists 받으면 silent skip (manual scan과 race 안전).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ssabro/rosshield/internal/app/scanrun"
	"github.com/ssabro/rosshield/internal/domain/benchmark"
	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/scheduler"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// fleetScanJobIDPrefix는 cron job ID prefix입니다 — `fleet-scan-<fleetId>` 형식.
const fleetScanJobIDPrefix = "fleet-scan-"

// FleetScanScheduler는 fleet-policy ScanSchedule cron job 등록/해제를 관리합니다.
//
// bootstrap에서 1회 생성 + handlers.Deps로 주입. handler가 fleet mutation 후 Reconcile/Cancel 호출.
type FleetScanScheduler struct {
	storage   storage.Storage
	scan      scan.Service
	robot     robot.Service
	benchmark benchmark.Service
	scanRun   *scanrun.Orchestrator
	sch       scheduler.Scheduler
	logger    *slog.Logger
}

// NewFleetScanScheduler는 새 FleetScanScheduler를 생성합니다.
//
// scanRun nil이면 모든 메서드가 no-op (cron tick이 trigger해도 실 work 없음 — 부팅 단순화).
func NewFleetScanScheduler(
	store storage.Storage,
	robotSvc robot.Service,
	benchmarkSvc benchmark.Service,
	scanSvc scan.Service,
	scanRun *scanrun.Orchestrator,
	sch scheduler.Scheduler,
	logger *slog.Logger,
) *FleetScanScheduler {
	return &FleetScanScheduler{
		storage:   store,
		scan:      scanSvc,
		robot:     robotSvc,
		benchmark: benchmarkSvc,
		scanRun:   scanRun,
		sch:       sch,
		logger:    logger,
	}
}

// RegisterAll은 모든 tenant의 활성 fleet을 walk해 ScanSchedule cron job을 등록합니다.
//
// 부팅 시점 1회 호출. 등록 실패는 logger.Warn으로 기록하고 다른 fleet 진행 계속 (best-effort).
func (s *FleetScanScheduler) RegisterAll(ctx context.Context) error {
	if s == nil || s.scanRun == nil {
		if s != nil && s.logger != nil {
			s.logger.Info("fleet-scan-scheduler: scanRun not wired, skipping registration")
		}
		return nil
	}

	tenantIDs, err := listAllTenantIDs(ctx, s.storage)
	if err != nil {
		return fmt.Errorf("fleet-scan-scheduler: list tenants: %w", err)
	}

	registered := 0
	for _, tid := range tenantIDs {
		fleets, err := listFleetsForTenant(ctx, s.storage, s.robot, tid)
		if err != nil {
			s.logger.Warn("fleet-scan-scheduler: list fleets failed",
				"tenantId", string(tid), "err", err.Error())
			continue
		}
		for _, f := range fleets {
			if s.registerFromFleet(tid, f) {
				registered++
			}
		}
	}
	s.logger.Info("fleet-scan-scheduler: registration done", "count", registered)
	return nil
}

// Reconcile은 단일 fleet의 cron job을 최신 정책 기준으로 재등록합니다.
//
// admin이 fleet을 Create/Update한 직후 호출 — 기존 등록 cancel 후 현재 정책 기준 재등록.
// fleet이 미존재(이미 deleted)면 cancel만. ScanSchedule/DefaultBaselineID 비어있으면 cancel만.
func (s *FleetScanScheduler) Reconcile(ctx context.Context, tenantID storage.TenantID, fleetID string) {
	if s == nil || s.scanRun == nil || s.sch == nil {
		return
	}
	// 기존 등록 cancel (없으면 no-op).
	s.sch.Cancel(fleetScanJobIDPrefix + fleetID)

	// 현재 fleet 정책 조회.
	var f robot.Fleet
	err := s.storage.Tx(storage.WithTenantID(ctx, tenantID), func(c context.Context, tx storage.Tx) error {
		got, e := s.robot.GetFleet(c, tx, fleetID)
		f = got
		return e
	})
	if err != nil {
		// 미존재 등 — cancel만 적용된 상태. 정상 동작.
		s.logger.Info("fleet-scan-scheduler: reconcile skipped (fleet not found or deleted)",
			"fleetId", fleetID, "tenantId", string(tenantID))
		return
	}
	s.registerFromFleet(tenantID, f)
}

// Cancel은 fleet의 cron job을 해제합니다.
//
// admin이 fleet을 Delete한 직후 호출. 미등록이면 no-op.
func (s *FleetScanScheduler) Cancel(fleetID string) {
	if s == nil || s.sch == nil {
		return
	}
	s.sch.Cancel(fleetScanJobIDPrefix + fleetID)
}

// registerFromFleet은 fleet 정책에서 cron 등록을 시도합니다 (정책 비면 skip).
//
// 반환값: 실제 등록되면 true (count 집계용).
func (s *FleetScanScheduler) registerFromFleet(tenantID storage.TenantID, f robot.Fleet) bool {
	spec := f.Policy.ScanSchedule
	packKey := f.Policy.DefaultBaselineID
	if spec == "" || packKey == "" {
		return false
	}
	jobID := fleetScanJobIDPrefix + f.ID
	if err := s.sch.Schedule(jobID, spec, s.makeJob(tenantID, f.ID, packKey)); err != nil {
		s.logger.Warn("fleet-scan-scheduler: register failed",
			"jobId", jobID, "fleetId", f.ID, "spec", spec, "err", err.Error())
		return false
	}
	s.logger.Info("fleet-scan-scheduler: registered",
		"jobId", jobID, "fleetId", f.ID, "spec", spec, "packKey", packKey)
	return true
}

// makeJob은 cron tick 시 실행될 클로저를 생성합니다.
//
// 동작:
//  1. tenant scope ctx 구성
//  2. robots × pack checks 사전 fetch (handler/scan.go preloadRobotsAndChecks와 같은 패턴)
//  3. scan.Service.StartScan 시도 → ErrFleetActiveScanExists면 silent skip (manual scan과 race 안전)
//  4. scanrun.Orchestrator.Run 동기 실행 — cron 자체가 background goroutine.
func (s *FleetScanScheduler) makeJob(tenantID storage.TenantID, fleetID, packKey string) func(context.Context) error {
	return func(ctx context.Context) error {
		txCtx := storage.WithTenantID(ctx, tenantID)

		var (
			session         scan.ScanSession
			preloadedRobots []scan.RobotTarget
			preloadedChecks []scan.CheckDef
		)
		err := s.storage.Tx(txCtx, func(c context.Context, tx storage.Tx) error {
			pk, e := s.benchmark.GetPackByKey(c, tx, tenantID, packKey)
			if e != nil {
				return fmt.Errorf("pack lookup: %w", e)
			}

			rs, e := s.robot.ListRobots(c, tx, fleetID)
			if e != nil {
				return fmt.Errorf("robot list: %w", e)
			}
			if len(rs) == 0 {
				return errFleetEmpty
			}
			for _, r := range rs {
				preloadedRobots = append(preloadedRobots, scan.RobotTarget{
					RobotID:      r.ID,
					Host:         r.Host,
					Port:         r.Port,
					AuthType:     string(r.AuthType),
					CredentialID: r.CredentialID,
				})
			}
			for _, c := range pk.Checks {
				preloadedChecks = append(preloadedChecks, scan.CheckDef{
					PackCheckID:  c.ID,
					Code:         c.CheckID,
					AuditCommand: []string{"bash", "-c", c.AuditCommand},
					TimeoutSec:   scan.DefaultCheckTimeoutSec,
					EvalRuleJSON: c.EvaluationRule,
				})
			}

			ses, e := s.scan.StartScan(c, tx, scan.StartScanRequest{
				FleetID: fleetID,
				PackID:  pk.ID,
				Trigger: scan.TriggerSchedule,
				Total:   len(rs) * len(pk.Checks),
			})
			if e != nil {
				return e
			}
			session = ses
			return nil
		})
		if err != nil {
			if errors.Is(err, scan.ErrFleetActiveScanExists) {
				s.logger.Info("fleet-scan-scheduler: active session exists, skipping",
					"fleetId", fleetID, "tenantId", string(tenantID))
				return nil
			}
			if errors.Is(err, errFleetEmpty) {
				s.logger.Debug("fleet-scan-scheduler: fleet has no robots, skipping",
					"fleetId", fleetID, "tenantId", string(tenantID))
				return nil
			}
			return err
		}

		_ = s.scanRun.Run(txCtx, tenantID, session.ID, preloadedRobots, preloadedChecks)
		s.logger.Info("fleet-scan-scheduler: triggered",
			"fleetId", fleetID, "sessionId", session.ID, "tenantId", string(tenantID))
		return nil
	}
}

// errFleetEmpty는 fleet에 robot 0개일 때 silent skip을 위한 sentinel입니다 (외부 노출 X).
var errFleetEmpty = errors.New("fleet has no robots")

// listAllTenantIDs는 bootstrap mode에서 모든 tenant ID를 raw SELECT로 가져옵니다.
//
// 본 호출은 cronsched 부팅 1회만 — runtime 영향 없음. tenant 격리 우회는 의도적
// (cron은 system-wide 단일 인스턴스이므로 모든 tenant fleet을 walk해야 함).
func listAllTenantIDs(ctx context.Context, store storage.Storage) ([]storage.TenantID, error) {
	var ids []storage.TenantID
	err := store.Bootstrap(ctx, func(c context.Context, tx storage.Tx) error {
		rows, e := tx.Query(c, `SELECT id FROM tenants WHERE id != 'system'`)
		if e != nil {
			return e
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id string
			if e := rows.Scan(&id); e != nil {
				return e
			}
			ids = append(ids, storage.TenantID(id))
		}
		return rows.Err()
	})
	return ids, err
}

// listFleetsForTenant는 tenant scope ListFleets wrapper입니다.
func listFleetsForTenant(ctx context.Context, store storage.Storage, robotSvc robot.Service, tenantID storage.TenantID) ([]robot.Fleet, error) {
	var out []robot.Fleet
	err := store.Tx(storage.WithTenantID(ctx, tenantID), func(c context.Context, tx storage.Tx) error {
		fs, e := robotSvc.ListFleets(c, tx)
		out = fs
		return e
	})
	return out, err
}
