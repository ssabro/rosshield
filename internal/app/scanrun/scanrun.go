// Package scanrun은 Scan 도메인을 SSH executor + check evaluator + EventBus와 결합하는
// application layer Orchestrator입니다 (E6 Stage D, R6-1).
//
// 책임:
//
//   - robots × checks 카티전 곱 fan-out
//   - worker pool (default 10, R4-4·R6-4) — golang.org/x/sync/semaphore
//   - 각 work item: SSHExecutor.Exec → CheckEvaluator.Evaluate → scan.Service.RecordResult
//   - 각 RecordResult 직후 EventBus publish "scan.progress"
//   - 모든 worker 완료 후 terminal 전이(completed/failed/cancelled) + "scan.completed"
//   - Cancel(sessionID): 진행 중 ctx 취소 + scan.Service.CancelSession (R4-5 — 진행 중은 timeout까지 완료 대기, 다음 item만 skip)
//
// 도메인 결합 규칙:
//
//	본 패키지는 scan·storage·eventbus·clock 만 import.
//	robot·benchmark·sshpool은 호출자(cmd/* bootstrap)가 어댑팅해서 scan.SSHExecutor·CheckEvaluator로 주입.
//	이로 인해 P5 도메인 격리가 application layer에서도 유지됨.
package scanrun

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// DefaultWorkerLimit는 동시 실행 worker 수 기본값입니다 (R4-4·R6-4 — Phase 1 고정 10).
const DefaultWorkerLimit = 10

// Deps는 Orchestrator의 의존성입니다.
type Deps struct {
	Scan      scan.Service
	Storage   storage.Storage
	Executor  scan.SSHExecutor
	Evaluator scan.CheckEvaluator
	Bus       eventbus.Bus
	Clock     clock.Clock

	// WorkerLimit은 한 Run 내 동시 worker 최대 수. 0이면 DefaultWorkerLimit.
	WorkerLimit int
}

// Orchestrator는 scan session의 fan-out 실행 + 결과 기록 + 이벤트 publish를 관장합니다.
//
// 동시 Run은 sessionID 단위로 격리됩니다 — 같은 sessionID 두 번 호출은 미정의(외부 보장).
type Orchestrator struct {
	deps Deps

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// New는 새 Orchestrator를 반환합니다.
func New(deps Deps) *Orchestrator {
	if deps.WorkerLimit <= 0 {
		deps.WorkerLimit = DefaultWorkerLimit
	}
	return &Orchestrator{
		deps:    deps,
		cancels: make(map[string]context.CancelFunc),
	}
}

// Run은 sessionID에 해당하는 ScanSession을 running으로 전이 후 robots × checks를 fan-out 실행합니다.
//
// 절차:
//
//  1. session = TransitionSession(sessionID, running)  → audit emit "scan.started"
//  2. queue = robots × checks 카티전 곱
//  3. 각 worker = SSH exec + evaluator + RecordResult + progress publish
//  4. wg.Wait → terminal 전이:
//     - ctx 정상 종료 → completed
//     - Cancel 호출됐거나 ctx cancel → cancelled (Cancel이 이미 DB 전이했으면 스킵)
//
// R4-5 시멘틱: Cancel 호출 시 새 work item은 skip하지만 진행 중 worker는 timeout까지 완료 대기.
// 모든 worker가 완료된 후에야 wg.Wait가 풀림.
//
// 빈 입력(robots 또는 checks 길이 0)은 즉시 completed로 전이.
func (o *Orchestrator) Run(ctx context.Context, tenantID storage.TenantID, sessionID string, robots []scan.RobotTarget, checks []scan.CheckDef) error {
	if len(robots) == 0 || len(checks) == 0 {
		// 빈 입력 — 빈 transitions: pending → running → completed (audit 2건 + scan.completed 1건).
		return o.runEmpty(ctx, tenantID, sessionID)
	}

	// 1. running 전이.
	if _, err := o.transitionTo(ctx, tenantID, sessionID, scan.StatusRunning, ""); err != nil {
		return fmt.Errorf("scanrun: transition to running: %w", err)
	}

	// 2. ctx + cancel 등록 — Cancel(sessionID) 호출 시 이 ctx 취소.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	o.mu.Lock()
	o.cancels[sessionID] = cancel
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		delete(o.cancels, sessionID)
		o.mu.Unlock()
	}()

	// 3. fan-out — semaphore로 동시성 제한.
	sem := semaphore.NewWeighted(int64(o.deps.WorkerLimit))
	var wg sync.WaitGroup

outer:
	for ri := range robots {
		for ci := range checks {
			// ctx 취소 시 새 work item skip — 이미 acquire한 worker는 wg.Wait가 완료 대기.
			if err := sem.Acquire(runCtx, 1); err != nil {
				break outer
			}
			r := robots[ri]
			c := checks[ci]
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer sem.Release(1)
				o.executeOne(runCtx, tenantID, sessionID, r, c)
			}()
		}
	}
	wg.Wait()

	// 4. terminal 전이.
	return o.finalize(ctx, tenantID, sessionID, runCtx.Err())
}

// Cancel은 in-flight session의 ctx를 취소하고 DB를 cancelled로 전이합니다.
//
// in-flight가 아니더라도 (이미 종료됐거나 시작 전) DB 전이는 시도. terminal이면 ErrInvalidTransition.
func (o *Orchestrator) Cancel(ctx context.Context, tenantID storage.TenantID, sessionID, reason string) error {
	o.mu.Lock()
	cancel, inFlight := o.cancels[sessionID]
	o.mu.Unlock()
	if inFlight {
		cancel()
	}
	txCtx := storage.WithTenantID(ctx, tenantID)
	return o.deps.Storage.Tx(txCtx, func(ctx context.Context, tx storage.Tx) error {
		_, err := o.deps.Scan.CancelSession(ctx, tx, sessionID, reason)
		return err
	})
}

// --- internals ---

func (o *Orchestrator) runEmpty(ctx context.Context, tenantID storage.TenantID, sessionID string) error {
	if _, err := o.transitionTo(ctx, tenantID, sessionID, scan.StatusRunning, ""); err != nil {
		return fmt.Errorf("scanrun: transition to running (empty): %w", err)
	}
	session, err := o.transitionTo(ctx, tenantID, sessionID, scan.StatusCompleted, "")
	if err != nil {
		return fmt.Errorf("scanrun: transition to completed (empty): %w", err)
	}
	o.publishCompleted(ctx, session, "")
	return nil
}

func (o *Orchestrator) executeOne(ctx context.Context, tenantID storage.TenantID, sessionID string, robot scan.RobotTarget, check scan.CheckDef) {
	timeout := time.Duration(check.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(scan.DefaultCheckTimeoutSec) * time.Second
	}

	var (
		outcome  scan.Outcome
		reason   string
		duration time.Duration
	)

	exec, err := o.deps.Executor.Exec(ctx, robot, check.AuditCommand, timeout)
	duration = exec.Duration
	if err != nil {
		outcome = scan.OutcomeError
		reason = fmt.Sprintf("ssh: %v", err)
	} else {
		eval, evalErr := o.deps.Evaluator.Evaluate(check.EvalRuleJSON, exec)
		if evalErr != nil {
			outcome = scan.OutcomeError
			reason = fmt.Sprintf("eval: %v", evalErr)
		} else {
			outcome = eval.Outcome
			reason = eval.Reason
		}
	}

	// RecordResult — 별도 Tx, ctx cancel 영향 받지 않게 background ctx 사용.
	// (R4-5 시멘틱: 진행 중 work는 timeout까지 완료해야 하고, 그 결과는 반드시 기록.)
	bgCtx := storage.WithTenantID(context.Background(), tenantID)
	recordCtx, recordCancel := context.WithTimeout(bgCtx, 5*time.Second)
	defer recordCancel()

	var session scan.ScanSession
	if err := o.deps.Storage.Tx(recordCtx, func(c context.Context, tx storage.Tx) error {
		if _, err := o.deps.Scan.RecordResult(c, tx, scan.RecordResultRequest{
			SessionID:   sessionID,
			RobotID:     robot.RobotID,
			CheckID:     check.Code,
			PackCheckID: check.PackCheckID,
			Outcome:     outcome,
			EvalReason:  reason,
			DurationMs:  duration.Milliseconds(),
			ExecutedAt:  o.deps.Clock.Now(),
		}); err != nil {
			return err
		}
		s, err := o.deps.Scan.GetSession(c, tx, sessionID)
		session = s
		return err
	}); err != nil {
		// 기록 실패는 silently swallow — 진행률 publish 안 함.
		// (errno 로그는 호출자 책임 — Phase 1은 단순화.)
		return
	}
	o.publishProgress(bgCtx, session)
}

func (o *Orchestrator) finalize(ctx context.Context, tenantID storage.TenantID, sessionID string, runCtxErr error) error {
	// 현재 DB 상태 조회 — Cancel이 이미 cancelled로 전이했을 수 있음.
	bgCtx := storage.WithTenantID(context.Background(), tenantID)
	var current scan.ScanSession
	if err := o.deps.Storage.Tx(bgCtx, func(c context.Context, tx storage.Tx) error {
		s, err := o.deps.Scan.GetSession(c, tx, sessionID)
		current = s
		return err
	}); err != nil {
		return fmt.Errorf("scanrun: lookup session: %w", err)
	}
	if current.Status.IsTerminal() {
		// 이미 terminal — Cancel·외부가 전이 완료. publish만.
		o.publishCompleted(ctx, current, current.FailureReason)
		if runCtxErr != nil {
			return runCtxErr
		}
		return nil
	}

	target := scan.StatusCompleted
	reason := ""
	if runCtxErr != nil {
		target = scan.StatusCancelled
		reason = "context cancelled"
	}
	finalSession, err := o.transitionTo(bgCtx, tenantID, sessionID, target, reason)
	if err != nil {
		return fmt.Errorf("scanrun: transition to %s: %w", target, err)
	}
	o.publishCompleted(ctx, finalSession, reason)
	if runCtxErr != nil {
		return runCtxErr
	}
	return nil
}

func (o *Orchestrator) transitionTo(ctx context.Context, tenantID storage.TenantID, sessionID string, target scan.SessionStatus, reason string) (scan.ScanSession, error) {
	txCtx := storage.WithTenantID(ctx, tenantID)
	var out scan.ScanSession
	if err := o.deps.Storage.Tx(txCtx, func(c context.Context, tx storage.Tx) error {
		s, err := o.deps.Scan.TransitionSession(c, tx, sessionID, target, reason)
		out = s
		return err
	}); err != nil {
		return scan.ScanSession{}, err
	}
	return out, nil
}

func (o *Orchestrator) publishProgress(ctx context.Context, session scan.ScanSession) {
	payload, _ := json.Marshal(scan.ProgressEventPayload{
		SessionID: session.ID,
		Total:     session.Progress.Total,
		Completed: session.Progress.Completed,
		Failed:    session.Progress.Failed,
	})
	_ = o.deps.Bus.Publish(ctx, eventbus.Event{
		Type:      scan.EventTypeProgress,
		Version:   1,
		TenantID:  string(session.TenantID),
		Aggregate: eventbus.AggregateRef{Type: scan.AggregateTypeScanSession, ID: session.ID},
		Payload:   payload,
	})
}

func (o *Orchestrator) publishCompleted(ctx context.Context, session scan.ScanSession, reason string) {
	payload, _ := json.Marshal(scan.CompletedEventPayload{
		SessionID: session.ID,
		Status:    string(session.Status),
		Reason:    reason,
		Total:     session.Progress.Total,
		Completed: session.Progress.Completed,
		Failed:    session.Progress.Failed,
	})
	_ = o.deps.Bus.Publish(ctx, eventbus.Event{
		Type:      scan.EventTypeCompleted,
		Version:   1,
		TenantID:  string(session.TenantID),
		Aggregate: eventbus.AggregateRef{Type: scan.AggregateTypeScanSession, ID: session.ID},
		Payload:   payload,
	})
}
