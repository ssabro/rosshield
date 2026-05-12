// Package scan은 ScanSession·ScanResult 도메인의 공개 표면을 정의합니다 (E6 Stage C).
//
// Phase 1 스코프: 도메인 격선만 — Orchestrator(SSH 결선·worker pool·Cancel 전파)는 Stage D.
// Stage C는 다음을 제공합니다:
//
//   - 모델: ScanSession (FSM) + ScanResult (5-값 outcome)
//   - Service: StartScan·GetSession·ListSessions·TransitionSession·CancelSession·RecordResult·ListResults
//   - sqliterepo 어댑터 + audit emit (scan.started·completed·failed·cancelled)
//
// 도메인 결합 규칙:
//
//	scan 도메인은 audit·robot·benchmark 패키지를 직접 import하지 않습니다 (P5 + depguard).
//	cmd/* bootstrap이 audit.Service 어댑터를 AuditEmitter로 주입.
//	robot·pack 참조는 ID 문자열만 받음 — 정합성 검증은 sqliterepo의 FK가 담당.
//
// 결정: R5-1~R5-7 (사용자 합의 2026-04-27).
package scan

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// SessionStatus는 ScanSession의 FSM 상태입니다 (R5-4).
//
// FSM:
//
//	pending  → running | failed | cancelled
//	running  → completed | failed | cancelled
//	completed | failed | cancelled  (terminal — no outgoing transition)
type SessionStatus string

const (
	StatusPending   SessionStatus = "pending"
	StatusRunning   SessionStatus = "running"
	StatusCompleted SessionStatus = "completed"
	StatusFailed    SessionStatus = "failed"
	StatusCancelled SessionStatus = "cancelled"
)

// SessionTrigger는 스캔 시작 트리거입니다 (R5-7).
type SessionTrigger string

const (
	TriggerManual   SessionTrigger = "manual"
	TriggerSchedule SessionTrigger = "schedule"
	TriggerEvent    SessionTrigger = "event"
)

// Outcome은 ScanResult의 5-값 결과입니다 (§07.2 + e6 deepdive §6).
//
// PASS / FAIL / INDETERMINATE — Evaluator(E4) 결과 그대로.
// ERROR — SSH 또는 evaluator execution failure.
// SKIPPED — differential mode hash match (Phase 1 후반).
type Outcome string

const (
	OutcomePass          Outcome = "pass"
	OutcomeFail          Outcome = "fail"
	OutcomeIndeterminate Outcome = "indeterminate"
	OutcomeError         Outcome = "error"
	OutcomeSkipped       Outcome = "skipped"
)

// SessionProgress는 (robot × check) 작업의 진행률입니다.
type SessionProgress struct {
	Total     int
	Completed int
	Failed    int
}

// ScanSession은 한 번의 스캔 실행 단위입니다 (§04.2).
type ScanSession struct {
	ID            string // "scan_<ULID>" (R5-1)
	TenantID      storage.TenantID
	FleetID       string
	PackID        string
	Trigger       SessionTrigger
	Status        SessionStatus
	Progress      SessionProgress
	FailureReason string // failed/cancelled 사유 (옵션)
	CreatedAt     time.Time
	UpdatedAt     time.Time
	StartedAt     *time.Time // pending → running 전이 시점
	CompletedAt   *time.Time // terminal 전이 시점
}

// ScanResult는 (session × robot × check)의 단일 결과입니다 (§04.2).
type ScanResult struct {
	ID          string // "scr_<ULID>" (R5-2)
	SessionID   string
	TenantID    storage.TenantID
	RobotID     string
	CheckID     string // 팩 내 식별자 (예: "CIS-1.1.1.1")
	PackCheckID string // pack_checks.id ("ck_<ULID>")
	Outcome     Outcome
	EvalReason  string
	DurationMs  int64
	ExecutedAt  time.Time
	CreatedAt   time.Time

	// PackKey는 ListResultsByRobot에서만 채워지는 derived 필드 — session→pack JOIN 결과.
	// 다른 ListResults·RecordResult 등에서는 빈 string. /packs/{packKey}/checks/{checkId}
	// navigation 용도 (Web UI ResultRow → check 상세 진입).
	PackKey string
	// SessionStartedAt도 ListResultsByRobot에서만 채워지는 derived 필드 — scan_sessions.started_at JOIN.
	// pending 상태 session의 결과는 nil(transition pending→running 직후만 set). Web UI SessionGroup 헤더 표시.
	SessionStartedAt *time.Time
	// SessionCompletedAt도 ListResultsByRobot에서만 채워지는 derived 필드 — scan_sessions.completed_at JOIN.
	// terminal 상태(completed/failed/cancelled) 전이 시점에 set. running session 결과는 nil.
	// Web UI SessionGroup 헤더 total duration(= completed - started) 계산용.
	SessionCompletedAt *time.Time
	// SessionFailureReason도 ListResultsByRobot에서만 채워지는 derived 필드 — scan_sessions.failure_reason JOIN.
	// failed 상태 전이 시 사유 기록. 그 외 status는 빈 string. Web UI SessionGroup 헤더 표시.
	SessionFailureReason string
	// SessionStatusEnriched도 ListResultsByRobot에서만 채워지는 derived 필드 — scan_sessions.status JOIN.
	// pending/running/completed/failed/cancelled 중 하나. Web UI SessionGroup 헤더 Badge 색상 분기 입력.
	SessionStatusEnriched SessionStatus
}

// TransitionTo는 FSM 검증 후 새 ScanSession 값을 반환합니다 (P9 불변성).
//
// 잘못된 전이는 ErrInvalidTransition. terminal 상태에서는 어떤 전이도 거부.
// pending → running: StartedAt 설정.
// → completed/failed/cancelled: CompletedAt 설정.
func (s ScanSession) TransitionTo(target SessionStatus, now time.Time) (ScanSession, error) {
	if !s.Status.canTransitionTo(target) {
		return ScanSession{}, fmt.Errorf("%w: %s → %s", ErrInvalidTransition, s.Status, target)
	}
	next := s
	next.Status = target
	next.UpdatedAt = now
	if target == StatusRunning && s.StartedAt == nil {
		st := now
		next.StartedAt = &st
	}
	if isTerminal(target) {
		ct := now
		next.CompletedAt = &ct
	}
	return next, nil
}

// IsTerminal은 status가 종착(completed/failed/cancelled) 상태인지 반환합니다.
func (s SessionStatus) IsTerminal() bool {
	return isTerminal(s)
}

func isTerminal(s SessionStatus) bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusCancelled
}

// canTransitionTo는 R5-4·R5-5 FSM을 인코딩합니다.
//
// pending → running·failed·cancelled (R5-5: pending에서도 cancel 가능)
// running → completed·failed·cancelled
// terminal → 거부
func (s SessionStatus) canTransitionTo(target SessionStatus) bool {
	switch s {
	case StatusPending:
		return target == StatusRunning ||
			target == StatusFailed ||
			target == StatusCancelled
	case StatusRunning:
		return target == StatusCompleted ||
			target == StatusFailed ||
			target == StatusCancelled
	default:
		return false
	}
}

// === Stage D — Orchestrator 결합 표면 (R6-2·R6-3·R6-6) ===
//
// scan 도메인은 application layer(`internal/app/scanrun/`)에 의해 결합됩니다.
// 도메인 자체는 외부 도메인(robot·benchmark·sshpool)을 import하지 않고,
// 호출자가 채워서 주입할 minimal struct + interface만 노출합니다.

// RobotTarget은 Orchestrator.Run 입력으로 전달되는 robot의 최소 식별·연결 정보입니다.
//
// robot.Robot의 부분 복제 — scan 패키지가 robot 도메인을 import하지 않도록 하기 위함(P5 + R6-2).
// 호출자(application service)가 robot.Service.GetRobot 결과로 채움.
type RobotTarget struct {
	RobotID      string
	Host         string
	Port         int
	AuthType     string // "password" | "privateKey" — 도메인 격리 위해 string
	CredentialID string // 호출자가 GetCredentialMaterial을 적시 호출
}

// CheckDef는 한 check의 audit command + 평가 규칙입니다.
//
// pack_checks의 부분 복제 — scan 패키지가 benchmark 도메인을 import하지 않도록 하기 위함(P5 + R6-2).
// 호출자(application service)가 pack 자료에서 채움.
type CheckDef struct {
	PackCheckID  string   // pack_checks.id ("ck_<ULID>")
	Code         string   // "CIS-1.1.1.1" — 팩 내 식별자
	AuditCommand []string // SSH exec argv
	TimeoutSec   int      // SSH exec timeout. 0이면 DefaultCheckTimeoutSec.
	EvalRuleJSON []byte   // 평가 규칙 AST JSON — CheckEvaluator에 위임
}

// DefaultCheckTimeoutSec는 CheckDef.TimeoutSec=0일 때 기본 SSH exec timeout (§07.7).
const DefaultCheckTimeoutSec = 10

// ExecResult는 SSH exec 결과입니다.
//
// sshpool.ExecResult의 도메인 격리 사본 — scan 패키지가 sshpool 도메인을 import하지 않도록.
type ExecResult struct {
	Stdout, Stderr []byte
	ExitCode       int
	Duration       time.Duration
}

// EvalResult는 CheckEvaluator.Evaluate의 출력입니다.
//
// Outcome은 본 패키지 5-값 enum 중 하나(pass·fail·indeterminate·error·skipped).
// Reason은 평가 사유 — pass/fail/indeterminate에서 의미 있고, error는 evaluator 실패 메시지.
type EvalResult struct {
	Outcome Outcome
	Reason  string
}

// SSHExecutor는 Orchestrator가 호출하는 SSH 실행 표면입니다 (R6-3).
//
// bootstrap이 sshpool.Executor를 어댑팅해 주입(robot 도메인의 GetCredentialMaterial 호출 결합 포함).
// 단위 테스트는 mock.
//
// 호출자는 ctx 취소 시 timeout보다 일찍 끝나야 함(R4-5는 진행 중 작업은 timeout까지 대기).
type SSHExecutor interface {
	Exec(ctx context.Context, target RobotTarget, argv []string, timeout time.Duration) (ExecResult, error)
}

// CheckEvaluator는 Orchestrator가 호출하는 평가 규칙 실행 표면입니다 (R6-3).
//
// bootstrap이 benchmark 도메인의 evaluator를 어댑팅해 주입.
// 단위 테스트는 mock.
type CheckEvaluator interface {
	Evaluate(ruleJSON []byte, exec ExecResult) (EvalResult, error)
}

// === EventBus 페이로드 (R6-6) ===

// 토픽 컨벤션 (R2-3 — `<domain>.<EventName>`).
const (
	EventTypeProgress  = "scan.progress"
	EventTypeCompleted = "scan.completed"
)

// AggregateTypeScanSession은 EventBus envelope의 aggregate.type 값입니다.
const AggregateTypeScanSession = "ScanSession"

// ProgressEventPayload는 `scan.progress` 이벤트 본문입니다.
//
// 한 RecordResult 직후 publish — 구독자(UI·CLI)가 진행률 모니터링 가능.
type ProgressEventPayload struct {
	SessionID string `json:"sessionId"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
}

// CompletedEventPayload는 `scan.completed` 이벤트 본문입니다.
//
// terminal 전이 직후 publish — Status는 completed/failed/cancelled 중 하나.
type CompletedEventPayload struct {
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`           // SessionStatus의 string화
	Reason    string `json:"reason,omitempty"` // failed/cancelled 사유
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
}

// AuditEmitter는 scan 도메인 변경을 감사 로그에 기록하는 콜백입니다 (P5 — audit 도메인 직접 import 회피).
//
// emit 시점:
//
//	pending → running    : EmitScanStarted
//	running → completed  : EmitScanCompleted
//	(pending|running) → failed    : EmitScanFailed
//	(pending|running) → cancelled : EmitScanCancelled
//
// StartScan 시점에는 audit emit 안 함 — 실제 실행이 시작되는 running 전이가 의미 있는 시점.
type AuditEmitter interface {
	EmitScanStarted(ctx context.Context, tx storage.Tx, s ScanSession) error
	EmitScanCompleted(ctx context.Context, tx storage.Tx, s ScanSession) error
	EmitScanFailed(ctx context.Context, tx storage.Tx, s ScanSession, reason string) error
	EmitScanCancelled(ctx context.Context, tx storage.Tx, s ScanSession, reason string) error
}

// StartScanRequest는 Service.StartScan 입력입니다.
//
// Total은 Orchestrator가 robot·check 카티전 곱 카운트로 산출 — Stage C는 외부에서 주입받음.
// Stage D Orchestrator가 본 메서드를 호출하기 전 robot·check 목록을 결정.
type StartScanRequest struct {
	FleetID string
	PackID  string
	Trigger SessionTrigger // 빈 값이면 TriggerManual.
	Total   int            // 예상 작업 수 (robot × check). 0이면 0으로 INSERT.
}

// ListSessionsFilter는 Service.ListSessions의 필터입니다.
//
// 모든 필드는 optional — 빈 값은 해당 차원의 필터링을 생략.
// Limit=0이면 default 50.
type ListSessionsFilter struct {
	FleetID string
	Status  SessionStatus
	Limit   int
}

// RecordResultRequest는 Service.RecordResult 입력입니다.
//
// Stage C는 RecordResult를 도메인 표면으로 노출만 — 실제 호출은 Stage D Orchestrator가 검사 완료마다.
// 같은 (SessionID, RobotID, CheckID)로 두 번 호출 시 ErrResultDuplicate.
type RecordResultRequest struct {
	SessionID   string
	RobotID     string
	CheckID     string
	PackCheckID string
	Outcome     Outcome
	EvalReason  string
	DurationMs  int64
	ExecutedAt  time.Time
}

// Service는 scan 도메인 진입점입니다 (E6 Stage C — 도메인 격선만).
//
// Stage D에서 Orchestrator·SSH 결선이 본 인터페이스를 호출하는 형태로 결합됩니다.
type Service interface {
	// StartScan은 새 ScanSession을 pending 상태로 생성합니다.
	// audit emit은 이 시점이 아닌 running 전이 시점 (TransitionSession 또는 별도 호출).
	StartScan(ctx context.Context, tx storage.Tx, req StartScanRequest) (ScanSession, error)

	// GetSession은 ID로 세션을 조회합니다. 없으면 storage.ErrNotFound.
	GetSession(ctx context.Context, tx storage.Tx, id string) (ScanSession, error)

	// ListSessions는 tenant 내 세션을 created_at DESC로 반환합니다 (R5-6).
	ListSessions(ctx context.Context, tx storage.Tx, filter ListSessionsFilter) ([]ScanSession, error)

	// TransitionSession은 FSM 전이를 적용하고 적절한 audit 이벤트를 emit합니다.
	// 잘못된 전이는 ErrInvalidTransition.
	// reason은 failed/cancelled에만 사용 — 다른 전이에서는 무시.
	TransitionSession(ctx context.Context, tx storage.Tx, id string, target SessionStatus, reason string) (ScanSession, error)

	// CancelSession은 TransitionSession(.., StatusCancelled, reason)의 의미론적 wrapper입니다 (R5-5).
	// pending·running 둘 다 cancel 가능. 이미 terminal이면 ErrInvalidTransition.
	CancelSession(ctx context.Context, tx storage.Tx, id string, reason string) (ScanSession, error)

	// RecordResult는 (session, robot, check) 결과를 INSERT하고 진행률을 갱신합니다.
	// 같은 키로 두 번 호출 시 ErrResultDuplicate.
	// session.Status != running이면 ErrSessionNotRunning.
	RecordResult(ctx context.Context, tx storage.Tx, req RecordResultRequest) (ScanResult, error)

	// ListResults는 세션의 모든 결과를 created_at ASC로 반환합니다.
	ListResults(ctx context.Context, tx storage.Tx, sessionID string) ([]ScanResult, error)

	// ListResultsByRobot은 robot의 최근 scan results를 executed_at DESC로 반환합니다.
	// limit <= 0이면 default 50. tenant scope.
	ListResultsByRobot(ctx context.Context, tx storage.Tx, robotID string, limit int) ([]ScanResult, error)
}

// 공통 에러.
var (
	// ScanSession 검증 에러.
	ErrSessionEmptyFleet     = errors.New("scan: FleetID is required")
	ErrSessionEmptyPack      = errors.New("scan: PackID is required")
	ErrSessionInvalidTrigger = errors.New("scan: Trigger must be one of manual|schedule|event")
	ErrSessionNegativeTotal  = errors.New("scan: Total must be >= 0")

	// FSM 에러.
	ErrInvalidTransition = errors.New("scan: invalid status transition")

	// Result 검증 에러.
	ErrResultEmptyRobot     = errors.New("scan: RobotID is required")
	ErrResultEmptyCheck     = errors.New("scan: CheckID is required")
	ErrResultEmptyPackCheck = errors.New("scan: PackCheckID is required")
	ErrResultInvalidOutcome = errors.New("scan: Outcome must be one of pass|fail|indeterminate|error|skipped")
	ErrResultDuplicate      = errors.New("scan: Result already recorded for (session, robot, check)")
	ErrSessionNotRunning    = errors.New("scan: session is not in running state — cannot record results")

	// 외래 자원 미존재.
	ErrFleetNotFound = errors.New("scan: Fleet not found or deleted")
	ErrPackNotFound  = errors.New("scan: Pack not found")
	ErrRobotNotFound = errors.New("scan: Robot not found or deleted")

	// 동시성 제약.
	// 같은 fleet에 이미 pending/running 세션이 있으면 새 StartScan 거부 — 자원 충돌·랜덤
	// scheduler 중복 실행 방지. 기존 세션을 cancel/완료해야 새로 시작 가능.
	ErrFleetActiveScanExists = errors.New("scan: an active scan session already exists for this fleet")
)

// ValidateOutcome는 Outcome 값이 5-값 enum 중 하나인지 검증합니다.
func ValidateOutcome(o Outcome) error {
	switch o {
	case OutcomePass, OutcomeFail, OutcomeIndeterminate, OutcomeError, OutcomeSkipped:
		return nil
	default:
		return ErrResultInvalidOutcome
	}
}

// ValidateTrigger는 SessionTrigger 값이 3-값 enum 중 하나인지 검증합니다.
func ValidateTrigger(t SessionTrigger) error {
	switch t {
	case TriggerManual, TriggerSchedule, TriggerEvent:
		return nil
	default:
		return ErrSessionInvalidTrigger
	}
}
