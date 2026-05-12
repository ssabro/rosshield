package main

// fleet_scan_scheduler.go — FleetPolicy.ScanSchedule cron 결선 (scheduler-driven scan).
//
// 책임:
//   - 부팅 시 모든 tenant의 모든 활성 fleet을 walk
//   - ScanSchedule(cron spec)이 비지 않고 DefaultBaselineID(pack key)가 비지 않으면
//     scheduler에 "fleet-scan-<fleetId>" 등록
//   - cron tick 시: pre-check active session(있으면 silent skip) → robots × pack checks 사전 fetch →
//     scan.Service.StartScan → scanrun.Orchestrator.Run
//
// HA 결합: cronsched RoleProvider gate가 follower tick을 silent skip — leader 단일 인스턴스만 자동 scan.
// 동시성 가드: scan.ErrFleetActiveScanExists 받으면 silent skip (manual scan과 race 안전).
//
// 한계 (별 epic):
//   - dynamic re-registration: fleet 등록·이름 변경·삭제 후에는 server restart 필요. 본 stage는 부팅 시점 1회 등록만.
//   - tenant 단위 분리 cron 도구는 없음 — 모든 tenant가 같은 cronsched 인스턴스 공유 (HA leader 1개).

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

// fleetScanDeps는 fleet scan job의 의존성입니다.
type fleetScanDeps struct {
	storage   storage.Storage
	scan      scan.Service
	robot     robot.Service
	benchmark benchmark.Service
	scanRun   *scanrun.Orchestrator
	logger    *slog.Logger
}

// registerFleetScanJobs는 모든 tenant의 활성 fleet을 walk해 ScanSchedule cron job을 등록합니다.
//
// 절차:
//  1. 모든 tenant ID 조회 (raw SELECT — bootstrap mode, cross-tenant 격리 우회)
//  2. 각 tenant scope로 ListFleets → 정책에서 ScanSchedule + DefaultBaselineID 추출
//  3. 둘 다 비지 않으면 cron 등록 (id = "fleet-scan-<fleetId>")
//
// 등록 실패는 logger.Warn으로 기록하고 다른 fleet 진행 계속 (best-effort).
func registerFleetScanJobs(
	ctx context.Context,
	store storage.Storage,
	robotSvc robot.Service,
	benchmarkSvc benchmark.Service,
	scanSvc scan.Service,
	scanRun *scanrun.Orchestrator,
	sch scheduler.Scheduler,
	logger *slog.Logger,
) error {
	if scanRun == nil {
		// scanRun 결선이 없으면 cron이 trigger해도 실 work 진행 X. 등록 의미 없음.
		logger.Info("fleet-scan-scheduler: scanRun not wired, skipping registration")
		return nil
	}

	tenantIDs, err := listAllTenantIDs(ctx, store)
	if err != nil {
		return fmt.Errorf("fleet-scan-scheduler: list tenants: %w", err)
	}

	deps := &fleetScanDeps{
		storage:   store,
		scan:      scanSvc,
		robot:     robotSvc,
		benchmark: benchmarkSvc,
		scanRun:   scanRun,
		logger:    logger,
	}

	registered := 0
	for _, tid := range tenantIDs {
		fleets, err := listFleetsForTenant(ctx, store, robotSvc, tid)
		if err != nil {
			logger.Warn("fleet-scan-scheduler: list fleets failed",
				"tenantId", string(tid), "err", err.Error())
			continue
		}
		for _, f := range fleets {
			spec := f.Policy.ScanSchedule
			packKey := f.Policy.DefaultBaselineID
			if spec == "" || packKey == "" {
				continue
			}
			jobID := fleetScanJobIDPrefix + f.ID
			fleetID := f.ID
			tenantID := tid
			if err := sch.Schedule(jobID, spec, makeFleetScanJob(deps, tenantID, fleetID, packKey)); err != nil {
				logger.Warn("fleet-scan-scheduler: register failed",
					"jobId", jobID, "fleetId", fleetID, "spec", spec, "err", err.Error())
				continue
			}
			logger.Info("fleet-scan-scheduler: registered",
				"jobId", jobID, "fleetId", fleetID, "spec", spec, "packKey", packKey)
			registered++
		}
	}
	logger.Info("fleet-scan-scheduler: registration done", "count", registered)
	return nil
}

// makeFleetScanJob은 cron tick 시 실행될 클로저를 생성합니다.
//
// 동작:
//  1. tenant scope ctx 구성
//  2. robots × pack checks 사전 fetch (handler/scan.go preloadRobotsAndChecks와 같은 패턴)
//  3. scan.Service.StartScan 시도 → ErrFleetActiveScanExists면 silent skip (manual scan과 race 안전)
//  4. scanrun.Orchestrator.Run 비동기 X — cron 자체가 background goroutine. 동기 실행 OK.
func makeFleetScanJob(deps *fleetScanDeps, tenantID storage.TenantID, fleetID, packKey string) func(context.Context) error {
	return func(ctx context.Context) error {
		txCtx := storage.WithTenantID(ctx, tenantID)

		var (
			session         scan.ScanSession
			preloadedRobots []scan.RobotTarget
			preloadedChecks []scan.CheckDef
		)
		err := deps.storage.Tx(txCtx, func(c context.Context, tx storage.Tx) error {
			// 1. pack 조회 (key → ID + checks).
			pk, e := deps.benchmark.GetPackByKey(c, tx, tenantID, packKey)
			if e != nil {
				return fmt.Errorf("pack lookup: %w", e)
			}
			// systemTenant 폴백: tenant scope에서 못 찾으면 system pack 검색.
			// (현재는 tenant scope만 — system pack 활용 시 별 시도 필요. Phase 1 단순화.)

			// 2. robots 조회.
			rs, e := deps.robot.ListRobots(c, tx, fleetID)
			if e != nil {
				return fmt.Errorf("robot list: %w", e)
			}
			if len(rs) == 0 {
				// robot 0 fleet은 silent skip (의미 있는 scan 0).
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

			// 3. StartScan 시도 — active session 있으면 ErrFleetActiveScanExists 받음 (silent skip).
			s, e := deps.scan.StartScan(c, tx, scan.StartScanRequest{
				FleetID: fleetID,
				PackID:  pk.ID,
				Trigger: scan.TriggerSchedule,
				Total:   len(rs) * len(pk.Checks),
			})
			if e != nil {
				return e
			}
			session = s
			return nil
		})
		if err != nil {
			if errors.Is(err, scan.ErrFleetActiveScanExists) {
				deps.logger.Info("fleet-scan-scheduler: active session exists, skipping",
					"fleetId", fleetID, "tenantId", string(tenantID))
				return nil
			}
			if errors.Is(err, errFleetEmpty) {
				deps.logger.Debug("fleet-scan-scheduler: fleet has no robots, skipping",
					"fleetId", fleetID, "tenantId", string(tenantID))
				return nil
			}
			return err
		}

		// 4. orchestrator 호출 (별 goroutine 안 만듦 — cron tick이 이미 background goroutine).
		// session.ID 안전 (정상 INSERT 후 반환). Run은 자체적으로 audit/event 처리.
		_ = deps.scanRun.Run(txCtx, tenantID, session.ID, preloadedRobots, preloadedChecks)
		deps.logger.Info("fleet-scan-scheduler: triggered",
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
