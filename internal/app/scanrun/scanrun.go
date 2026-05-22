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

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/semaphore"

	"github.com/ssabro/rosshield/internal/domain/evidence"
	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// DefaultWorkerLimit는 동시 실행 worker 수 기본값입니다 (R4-4·R6-4 — Phase 1 고정 10).
const DefaultWorkerLimit = 10

// DefaultHealthFailureThreshold는 per-robot health window가 발동하는 연속 실패 횟수입니다
// (scanrun SSH 통합 Stage 5 — D-SCAN-7 + e6 deepdive §5.6 패턴).
//
// 한 robot에 대해 SSH exec가 연속 N회 실패하면 잔여 check는 즉시 OutcomeSkipped
// (reason="robot_offline")로 처리 — robot 부재 시 timeout × check 수만큼 대기 회피.
// 본 robot에 한 번이라도 success가 들어오면 카운터 reset (false-positive 회피).
const DefaultHealthFailureThreshold = 3

// Deps는 Orchestrator의 의존성입니다.
type Deps struct {
	Scan      scan.Service
	Storage   storage.Storage
	Executor  scan.SSHExecutor
	Evaluator scan.CheckEvaluator
	Bus       eventbus.Bus
	Clock     clock.Clock

	// Evidence는 SSH stdout/stderr를 redact·해시·blob 영속하고 N:M ref를 부착합니다 (E7 Stage C).
	// nil이면 evidence 기록을 skip — bootstrap이 아직 결선 안 한 단위 테스트 호환.
	Evidence evidence.Service

	// WorkerLimit은 한 Run 내 동시 worker 최대 수. 0이면 DefaultWorkerLimit.
	WorkerLimit int

	// CheckTimeoutDefaultSec는 CheckDef.TimeoutSec=0일 때 적용할 default. 0이면
	// scan.DefaultCheckTimeoutSec(10초). 운영자가 customer 환경에 맞춰 조정 가능
	// (긴 합성 bash 또는 빠른 fail-fast 정책). per-check TimeoutSec은 항상 우선.
	CheckTimeoutDefaultSec int

	// HealthFailureThreshold는 per-robot health window 발동 임계값입니다
	// (scanrun SSH 통합 Stage 5). 0이면 DefaultHealthFailureThreshold(3).
	// 한 robot의 SSH exec가 연속 N회 실패하면 잔여 check OutcomeSkipped(robot_offline).
	HealthFailureThreshold int

	// Tracer는 OpenTelemetry tracer 입니다 (Phase 11.A-4). nil이면 noop tracer 사용 — overhead 0.
	//
	// bootstrap은 platformotel.Provider.Tracer("rosshield/scanrun") 으로 주입.
	// Enabled=false 시 platformotel.Provider가 noop tracer 를 반환하므로 별도 분기 불필요.
	Tracer trace.Tracer
}

// Orchestrator는 scan session의 fan-out 실행 + 결과 기록 + 이벤트 publish를 관장합니다.
//
// 동시 Run은 sessionID 단위로 격리됩니다 — 같은 sessionID 두 번 호출은 미정의(외부 보장).
type Orchestrator struct {
	deps Deps

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// healthState는 한 robot의 연속 실패 카운터입니다 (Stage 5 per-robot health window).
//
// scope는 Run 단일 호출 — Run 종료 시 Orchestrator.healthFor 맵에서 GC.
// 같은 session 내 robots × checks fan-out에서 robot 별로 누적.
type healthState struct {
	mu                  sync.Mutex
	consecutiveFailures int
}

func (h *healthState) recordFailure() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.consecutiveFailures++
	return h.consecutiveFailures
}

func (h *healthState) recordSuccess() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.consecutiveFailures = 0
}

func (h *healthState) shouldSkip(threshold int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.consecutiveFailures >= threshold
}

// New는 새 Orchestrator를 반환합니다.
func New(deps Deps) *Orchestrator {
	if deps.WorkerLimit <= 0 {
		deps.WorkerLimit = DefaultWorkerLimit
	}
	if deps.HealthFailureThreshold <= 0 {
		deps.HealthFailureThreshold = DefaultHealthFailureThreshold
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
	// Phase 11.A-4 — parent scan.run span. 모든 worker 의 5 child span 이 본 span 의 ctx 를 상속.
	// noop tracer 시 Start 비용 ≈ 0 (no allocation).
	ctx, runSpan := o.startScanRunSpan(ctx, sessionID, string(tenantID), len(robots), len(checks))
	defer runSpan.End()

	if len(robots) == 0 || len(checks) == 0 {
		// 빈 입력 — 빈 transitions: pending → running → completed (audit 2건 + scan.completed 1건).
		runSpan.SetAttributes(attribute.String(attrScanStatus, "completed_empty"))
		return o.runEmpty(ctx, tenantID, sessionID)
	}

	// 1. running 전이.
	if _, err := o.transitionTo(ctx, tenantID, sessionID, scan.StatusRunning, ""); err != nil {
		recordSpanErr(runSpan, err)
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
	// worker ctx에 tenant 세팅 — SSHExecutor 어댑터가 storage.Tx로 GetCredentialMaterial 호출 시 필요.
	workerCtx := storage.WithTenantID(runCtx, tenantID)
	sem := semaphore.NewWeighted(int64(o.deps.WorkerLimit))
	var wg sync.WaitGroup

	// Stage 5 — per-robot health window. Run scope 격리 (다음 Run은 새 map).
	// robotID → *healthState. sync.Map은 robot 별 LoadOrStore + 동시 worker write 안전.
	var healthMap sync.Map

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
				o.executeOne(workerCtx, tenantID, sessionID, r, c, &healthMap)
			}()
		}
	}
	wg.Wait()

	// 4. terminal 전이.
	err := o.finalize(ctx, tenantID, sessionID, runCtx.Err())
	// Phase 11.A-4 — parent scan.run span 의 status attribute 갱신.
	o.annotateRunSpan(runSpan, runCtx.Err(), err)
	return err
}

// annotateRunSpan 은 finalize 종료 후 parent scan.run span 의 attribute + status 를 보정합니다.
//
// runCtxErr 가 non-nil 이면 cancelled(또는 deadline) — span 은 OK 유지(정상 종료 시나리오).
// finalizeErr 가 non-nil 이면 transition 실패 — span.RecordError + status=Error.
func (o *Orchestrator) annotateRunSpan(span trace.Span, runCtxErr, finalizeErr error) {
	switch {
	case finalizeErr != nil:
		recordSpanErr(span, finalizeErr)
		span.SetAttributes(attribute.String(attrScanStatus, "failed"))
	case runCtxErr != nil:
		span.SetAttributes(attribute.String(attrScanStatus, "cancelled"))
	default:
		span.SetAttributes(attribute.String(attrScanStatus, "completed"))
	}
}

// Cancel은 in-flight session의 ctx를 취소하고 DB를 cancelled로 전이합니다.
//
// in-flight가 아니더라도 (이미 종료됐거나 시작 전) DB 전이는 시도. terminal이면 ErrInvalidTransition.
// terminal 전이 직후 같은 Tx 안에서 RecomputeSeverityAggregate 호출 — atomic 일관성(D26 §5.4).
// 갱신된 ScanSession + 에러 반환 — 호출자가 응답 직렬화에 사용. SeverityFailed 4 컬럼은 재조회로 채움.
func (o *Orchestrator) Cancel(ctx context.Context, tenantID storage.TenantID, sessionID, reason string) (scan.ScanSession, error) {
	o.mu.Lock()
	cancel, inFlight := o.cancels[sessionID]
	o.mu.Unlock()
	if inFlight {
		cancel()
	}
	var session scan.ScanSession
	txCtx := storage.WithTenantID(ctx, tenantID)
	err := o.deps.Storage.Tx(txCtx, func(ctx context.Context, tx storage.Tx) error {
		if _, e := o.deps.Scan.CancelSession(ctx, tx, sessionID, reason); e != nil {
			return e
		}
		if e := o.deps.Scan.RecomputeSeverityAggregate(ctx, tx, sessionID); e != nil {
			return fmt.Errorf("scanrun: recompute severity aggregate (cancel): %w", e)
		}
		// CancelSession 반환값은 재계산 전 상태 — 4 컬럼이 모두 0. 재조회로 갱신값 채움.
		s, e := o.deps.Scan.GetSession(ctx, tx, sessionID)
		if e != nil {
			return fmt.Errorf("scanrun: re-read session after recompute (cancel): %w", e)
		}
		session = s
		return nil
	})
	return session, err
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

func (o *Orchestrator) executeOne(ctx context.Context, tenantID storage.TenantID, sessionID string, robot scan.RobotTarget, check scan.CheckDef, healthMap *sync.Map) {
	timeout := o.resolveTimeout(check)

	// Stage 5 — per-robot health window 체크.
	// robot이 이미 N회 연속 실패면 SSH dial 회피 + 즉시 OutcomeSkipped record.
	hsRaw, _ := healthMap.LoadOrStore(robot.RobotID, &healthState{})
	health := hsRaw.(*healthState)

	// Phase 11.A-4 — SSH connect + check exec + evaluate 5 span 중 3 개. parent 는 scan.run.
	outcome, reason, duration, exec := o.runRemoteCheck(ctx, robot, check, timeout, health)

	// RecordResult + Evidence 기록 — 같은 Tx에 atomic.
	// 별도 Tx + background ctx (R4-5: ctx cancel 영향 받지 않게).
	bgCtx := storage.WithTenantID(context.Background(), tenantID)
	recordCtx, recordCancel := context.WithTimeout(bgCtx, 5*time.Second)
	defer recordCancel()
	// span ctx 는 worker ctx(=parent scan.run) 기준으로 child 가 nest 되도록 분리 보관.
	spanCtx := ctx

	session, err := o.recordAndPublish(recordCtx, spanCtx, recordOneInput{
		tenantID:  tenantID,
		sessionID: sessionID,
		robot:     robot,
		check:     check,
		exec:      exec,
		outcome:   outcome,
		reason:    reason,
		duration:  duration,
	})
	if err != nil {
		// 기록 실패는 silently swallow — 진행률 publish 안 함.
		// (errno 로그는 호출자 책임 — Phase 1은 단순화.)
		return
	}
	o.publishProgressSpan(spanCtx, bgCtx, session, robot.RobotID, check.PackCheckID)
}

// resolveTimeout 은 CheckDef.TimeoutSec 우선 + bootstrap config fallback + const fallback.
func (o *Orchestrator) resolveTimeout(check scan.CheckDef) time.Duration {
	timeout := time.Duration(check.TimeoutSec) * time.Second
	if timeout > 0 {
		return timeout
	}
	defaultSec := o.deps.CheckTimeoutDefaultSec
	if defaultSec <= 0 {
		defaultSec = scan.DefaultCheckTimeoutSec
	}
	return time.Duration(defaultSec) * time.Second
}

// runRemoteCheck 는 SSH connect + exec + evaluate 3 단계 child span 을 emit 하며 outcome/reason/duration 을 결정합니다.
//
// span hierarchy (parent scan.run 아래):
//   - ssh.connect   (marker) — health window skip 이 아닐 때만 emit.
//   - check.exec    — Executor.Exec 호출 전체.
//   - check.evaluate — Evaluator.Evaluate (SSH 성공 시에만).
func (o *Orchestrator) runRemoteCheck(ctx context.Context, robot scan.RobotTarget, check scan.CheckDef, timeout time.Duration, health *healthState) (scan.Outcome, string, time.Duration, scan.ExecResult) {
	tr := o.tracer()
	checkMeta := checkInfo{PackCheckID: check.PackCheckID, Code: check.Code}

	if health.shouldSkip(o.deps.HealthFailureThreshold) {
		return scan.OutcomeSkipped, "robot_offline", 0, scan.ExecResult{}
	}

	// 1) ssh.connect marker — scan flow 가 ssh hop 을 시도한다는 표지.
	connCtx, connSpan := startSSHConnectSpan(ctx, tr, ssherTarget{RobotID: robot.RobotID, Host: robot.Host, Port: robot.Port})
	connSpan.End()

	// 2) check.exec — Executor.Exec 호출 전체. parent 는 ssh hop ctx 가 아닌 worker ctx (sibling).
	_ = connCtx // marker span 의 ctx 는 worker scope 에서 사용하지 않음 — End 후 sibling 으로 진행.
	execCtx, execSpan := startCheckExecSpan(ctx, tr, robot.RobotID, checkMeta)
	exec, err := o.deps.Executor.Exec(execCtx, robot, check.AuditCommand, timeout, scan.ExecOpts{RequiresSudo: check.RequiresSudo})
	annotateExecSpan(execSpan, exec, err)
	execSpan.End()
	if err != nil {
		health.recordFailure()
		return scan.OutcomeError, fmt.Sprintf("ssh: %v", err), exec.Duration, exec
	}

	// 3) check.evaluate — Evaluator.Evaluate. evaluator error 는 robot 헬스 무관 — 카운터 변경 X.
	outcome, reason := o.evaluateWithSpan(ctx, tr, robot.RobotID, checkMeta, check.EvalRuleJSON, exec)
	if outcome != scan.OutcomeError {
		// SSH 자체는 success — robot alive로 간주, 카운터 reset.
		health.recordSuccess()
	}
	return outcome, reason, exec.Duration, exec
}

// evaluateWithSpan 은 CheckEvaluator.Evaluate 호출을 check.evaluate span 으로 감쌉니다.
func (o *Orchestrator) evaluateWithSpan(ctx context.Context, tr trace.Tracer, robotID string, check checkInfo, ruleJSON []byte, exec scan.ExecResult) (scan.Outcome, string) {
	_, evalSpan := startCheckEvaluateSpan(ctx, tr, robotID, check)
	defer evalSpan.End()
	eval, evalErr := o.deps.Evaluator.Evaluate(ruleJSON, exec)
	if evalErr != nil {
		recordSpanErr(evalSpan, evalErr)
		evalSpan.SetAttributes(attribute.String(attrCheckOutcome, string(scan.OutcomeError)))
		return scan.OutcomeError, fmt.Sprintf("eval: %v", evalErr)
	}
	evalSpan.SetAttributes(attribute.String(attrCheckOutcome, string(eval.Outcome)))
	if eval.Reason != "" {
		evalSpan.SetAttributes(attribute.String(attrCheckReason, eval.Reason))
	}
	return eval.Outcome, eval.Reason
}

// annotateExecSpan 은 check.exec span 에 exit_code · duration_ms · error 를 부착합니다.
func annotateExecSpan(span trace.Span, exec scan.ExecResult, err error) {
	span.SetAttributes(
		attribute.Int(attrExecExitCode, exec.ExitCode),
		attribute.Int64(attrExecDurationMs, exec.Duration.Milliseconds()),
	)
	recordSpanErr(span, err)
}

// recordOneInput 은 recordAndPublish 의 입력 파라미터 묶음입니다 (함수 시그니처 단순화).
type recordOneInput struct {
	tenantID  storage.TenantID
	sessionID string
	robot     scan.RobotTarget
	check     scan.CheckDef
	exec      scan.ExecResult
	outcome   scan.Outcome
	reason    string
	duration  time.Duration
}

// recordAndPublish 는 evidence.write + scan.publish 2 child span 을 emit 하며 결과를 DB 에 영속합니다.
//
// recordCtx 는 background timeout 5s — R4-5 일관(ctx cancel 영향 받지 않음).
// spanCtx 는 worker scope (parent scan.run) — child span 의 nest 보장.
func (o *Orchestrator) recordAndPublish(recordCtx, spanCtx context.Context, in recordOneInput) (scan.ScanSession, error) {
	tr := o.tracer()

	// 1) evidence.write child span — Storage.Tx 안의 evidence Store + LinkToResult 합산.
	_, evidenceSpan := startEvidenceWriteSpan(spanCtx, tr, in.robot.RobotID, in.check.PackCheckID)
	// 2) scan.publish child span — RecordResult + Bus.Publish progress.
	_, publishSpan := startScanPublishSpan(spanCtx, tr, in.sessionID, in.robot.RobotID, in.check.PackCheckID)
	publishSpan.SetAttributes(attribute.String(attrCheckOutcome, string(in.outcome)))

	var session scan.ScanSession
	var evidenceBytes int64
	var evidenceCount int
	err := o.deps.Storage.Tx(recordCtx, func(c context.Context, tx storage.Tx) error {
		ev, err := o.storeEvidence(c, tx, in)
		if err != nil {
			return err
		}
		evidenceBytes = ev.bytes
		evidenceCount = ev.count

		result, err := o.deps.Scan.RecordResult(c, tx, scan.RecordResultRequest{
			SessionID:   in.sessionID,
			RobotID:     in.robot.RobotID,
			CheckID:     in.check.Code,
			PackCheckID: in.check.PackCheckID,
			Outcome:     in.outcome,
			EvalReason:  in.reason,
			DurationMs:  in.duration.Milliseconds(),
			ExecutedAt:  o.deps.Clock.Now(),
		})
		if err != nil {
			return err
		}
		if o.deps.Evidence != nil && len(ev.ids) > 0 {
			if _, err := o.deps.Evidence.LinkToResult(c, tx, result.ID, ev.ids); err != nil {
				return fmt.Errorf("evidence link: %w", err)
			}
		}
		s, err := o.deps.Scan.GetSession(c, tx, in.sessionID)
		session = s
		return err
	})
	evidenceSpan.SetAttributes(
		attribute.Int64(attrEvidenceBytes, evidenceBytes),
		attribute.Int(attrEvidenceCount, evidenceCount),
	)
	recordSpanErr(evidenceSpan, err)
	evidenceSpan.End()
	recordSpanErr(publishSpan, err)
	publishSpan.End()
	return session, err
}

// evidenceWriteResult 는 storeEvidence 의 출력입니다.
type evidenceWriteResult struct {
	ids   []string
	bytes int64
	count int
}

// storeEvidence 는 stdout/stderr blob 을 evidence.Service.Store 호출로 영속합니다.
//
// error outcome 시에도 stderr 가 비어있지 않으면 보존(forensic 가치).
// stdout 은 nil 일 수 있으나 OutcomeError 가 아닌 경우엔 empty blob 으로 record.
func (o *Orchestrator) storeEvidence(ctx context.Context, tx storage.Tx, in recordOneInput) (evidenceWriteResult, error) {
	var res evidenceWriteResult
	if o.deps.Evidence == nil {
		return res, nil
	}
	if in.exec.Stdout != nil || in.outcome != scan.OutcomeError {
		store, err := o.deps.Evidence.Store(ctx, tx, evidence.StoreInput{
			TenantID: in.tenantID, ContentType: evidence.ContentStdout, Raw: in.exec.Stdout,
		})
		if err != nil {
			return res, fmt.Errorf("evidence stdout: %w", err)
		}
		res.ids = append(res.ids, store.EvidenceID)
		res.bytes += int64(len(in.exec.Stdout))
		res.count++
	}
	if len(in.exec.Stderr) > 0 {
		store, err := o.deps.Evidence.Store(ctx, tx, evidence.StoreInput{
			TenantID: in.tenantID, ContentType: evidence.ContentStderr, Raw: in.exec.Stderr,
		})
		if err != nil {
			return res, fmt.Errorf("evidence stderr: %w", err)
		}
		res.ids = append(res.ids, store.EvidenceID)
		res.bytes += int64(len(in.exec.Stderr))
		res.count++
	}
	return res, nil
}

// publishProgressSpan 은 publishProgress 호출을 parent scan.run 의 직접 child 로 emit 합니다.
//
// recordAndPublish 의 scan.publish span 은 DB write 까지 — bus.Publish 는 별 marker.
// 본 helper 는 단순 wrapper, publishProgress 그대로 호출.
func (o *Orchestrator) publishProgressSpan(spanCtx, bgCtx context.Context, session scan.ScanSession, robotID, checkID string) {
	tr := o.tracer()
	_, span := tr.Start(spanCtx, spanScanRunPublish,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String(attrScanID, session.ID),
			attribute.String(attrRobotID, robotID),
			attribute.String(attrCheckID, checkID),
			attribute.Int(attrScanTotal, session.Progress.Total),
		),
	)
	defer span.End()
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

// transitionTo는 Service.TransitionSession을 단일 Tx에 wrap합니다.
//
// target이 terminal(completed/failed/cancelled)이면 같은 Tx 안에서 RecomputeSeverityAggregate
// 호출 — atomic 일관성 + list polling 비용 0(D26 §5.4). 비-terminal 전이(running)는 재계산 X.
// terminal 시 반환 ScanSession은 재조회로 SeverityFailed 4 필드 채움.
func (o *Orchestrator) transitionTo(ctx context.Context, tenantID storage.TenantID, sessionID string, target scan.SessionStatus, reason string) (scan.ScanSession, error) {
	txCtx := storage.WithTenantID(ctx, tenantID)
	var out scan.ScanSession
	if err := o.deps.Storage.Tx(txCtx, func(c context.Context, tx storage.Tx) error {
		s, err := o.deps.Scan.TransitionSession(c, tx, sessionID, target, reason)
		if err != nil {
			return err
		}
		out = s
		if !target.IsTerminal() {
			return nil
		}
		if err := o.deps.Scan.RecomputeSeverityAggregate(c, tx, sessionID); err != nil {
			return fmt.Errorf("scanrun: recompute severity aggregate: %w", err)
		}
		// TransitionSession 반환값은 재계산 전 — SeverityFailed 4 필드가 모두 0.
		// 재조회로 갱신된 row를 캡처해 호출자(API 응답·publishCompleted)에 정확한 값 전달.
		refreshed, err := o.deps.Scan.GetSession(c, tx, sessionID)
		if err != nil {
			return fmt.Errorf("scanrun: re-read session after recompute: %w", err)
		}
		out = refreshed
		return nil
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
