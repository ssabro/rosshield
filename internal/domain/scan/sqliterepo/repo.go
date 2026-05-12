// Package sqliterepo는 scan.Service의 SQLite 어댑터입니다 (E6 Stage C).
//
// 도메인 격선만 — Orchestrator(SSH 결선·worker pool·Cancel 전파)는 Stage D.
//
// 책임:
//
//	StartScan          → scan_sessions INSERT (pending), audit emit 없음
//	GetSession         → SELECT (tenant 격리)
//	ListSessions       → SELECT (filter + DESC, LIMIT)
//	TransitionSession  → FSM 검증 → UPDATE → audit emit (started/completed/failed/cancelled)
//	CancelSession      → TransitionSession(cancelled) 위임 (R5-5)
//	RecordResult       → INSERT scan_results + UPDATE progress (running 강제)
//	ListResults        → SELECT (session 단위)
//
// system pack (packs.tenant_id='system') 접근 허용 — 호출 tenant 또는 'system'.
package sqliterepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/scan"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// rfc3339Nano는 DB 시간 칼럼 직렬화 포맷입니다.
const rfc3339Nano = time.RFC3339Nano

// defaultListLimit는 ListSessions의 기본 limit입니다.
const defaultListLimit = 50

// Deps는 어댑터 의존성입니다.
type Deps struct {
	Clock clock.Clock
	IDGen idgen.IDGen
	Audit scan.AuditEmitter // bootstrap에서 audit.Service 어댑터 주입.
}

// Repo는 scan.Service의 SQLite 구현입니다.
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// StartScan은 새 ScanSession을 pending 상태로 INSERT합니다.
//
// Fleet 활성·Pack 접근 가능 검증 후 INSERT. audit emit은 이 시점이 아닌 running 전이 시점.
// Trigger 빈 값이면 manual 기본값 적용.
func (r *Repo) StartScan(ctx context.Context, tx storage.Tx, req scan.StartScanRequest) (scan.ScanSession, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return scan.ScanSession{}, storage.ErrTenantMissing
	}
	if err := validateStartScan(&req); err != nil {
		return scan.ScanSession{}, err
	}
	if err := assertFleetActive(ctx, tx, tenantID, req.FleetID); err != nil {
		return scan.ScanSession{}, err
	}
	if err := assertPackAccessible(ctx, tx, tenantID, req.PackID); err != nil {
		return scan.ScanSession{}, err
	}
	if err := assertNoActiveFleetSession(ctx, tx, tenantID, req.FleetID); err != nil {
		return scan.ScanSession{}, err
	}

	now := r.deps.Clock.Now().UTC()
	session := scan.ScanSession{
		ID:        r.deps.IDGen.New("scan"),
		TenantID:  tenantID,
		FleetID:   req.FleetID,
		PackID:    req.PackID,
		Trigger:   req.Trigger,
		Status:    scan.StatusPending,
		Progress:  scan.SessionProgress{Total: req.Total},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO scan_sessions (
    id, tenant_id, fleet_id, pack_id, trigger, status,
    progress_total, progress_completed, progress_failed,
    failure_reason, created_at, updated_at, started_at, completed_at
) VALUES (?, ?, ?, ?, ?, 'pending', ?, 0, 0, '', ?, ?, NULL, NULL)`,
		session.ID, string(session.TenantID), session.FleetID, session.PackID,
		string(session.Trigger), session.Progress.Total,
		session.CreatedAt.Format(rfc3339Nano), session.UpdatedAt.Format(rfc3339Nano),
	); err != nil {
		return scan.ScanSession{}, fmt.Errorf("scan: insert session: %w", err)
	}
	return session, nil
}

// GetSession은 ID로 세션을 조회합니다 (tenant 격리).
func (r *Repo) GetSession(ctx context.Context, tx storage.Tx, id string) (scan.ScanSession, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return scan.ScanSession{}, storage.ErrTenantMissing
	}
	row := tx.QueryRow(ctx, sessionSelectColumns+`
  FROM scan_sessions
 WHERE id = ? AND tenant_id = ?`,
		id, string(tenantID))
	return scanSessionRow(row.Scan)
}

// ListSessions는 tenant 내 세션을 created_at DESC로 반환합니다.
func (r *Repo) ListSessions(ctx context.Context, tx storage.Tx, filter scan.ListSessionsFilter) ([]scan.ScanSession, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}

	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(sessionSelectColumns)
	query.WriteString(`
  FROM scan_sessions
 WHERE tenant_id = ?`)
	args = append(args, string(tenantID))

	if filter.FleetID != "" {
		query.WriteString(` AND fleet_id = ?`)
		args = append(args, filter.FleetID)
	}
	if filter.Status != "" {
		query.WriteString(` AND status = ?`)
		args = append(args, string(filter.Status))
	}
	query.WriteString(` ORDER BY created_at DESC LIMIT ?`)
	args = append(args, limit)

	rows, err := tx.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("scan: list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []scan.ScanSession
	for rows.Next() {
		s, err := scanSessionRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan: list sessions iterate: %w", err)
	}
	return out, nil
}

// TransitionSession은 FSM 전이를 적용하고 적절한 audit 이벤트를 emit합니다.
//
// 전이 매핑:
//
//	pending → running    : EmitScanStarted
//	running → completed  : EmitScanCompleted
//	(pending|running) → failed    : EmitScanFailed (reason 포함)
//	(pending|running) → cancelled : EmitScanCancelled (reason 포함)
func (r *Repo) TransitionSession(ctx context.Context, tx storage.Tx, id string, target scan.SessionStatus, reason string) (scan.ScanSession, error) {
	current, err := r.GetSession(ctx, tx, id)
	if err != nil {
		return scan.ScanSession{}, err
	}
	now := r.deps.Clock.Now().UTC()
	next, err := current.TransitionTo(target, now)
	if err != nil {
		return scan.ScanSession{}, err
	}
	if target == scan.StatusFailed || target == scan.StatusCancelled {
		next.FailureReason = reason
	}

	if err := updateSessionStatus(ctx, tx, next); err != nil {
		return scan.ScanSession{}, fmt.Errorf("scan: update session: %w", err)
	}

	if r.deps.Audit != nil {
		if err := emitTransition(ctx, tx, r.deps.Audit, current.Status, next, reason); err != nil {
			return scan.ScanSession{}, fmt.Errorf("scan: emit audit: %w", err)
		}
	}
	return next, nil
}

// CancelSession은 TransitionSession(.., StatusCancelled, reason) wrapper입니다 (R5-5).
func (r *Repo) CancelSession(ctx context.Context, tx storage.Tx, id, reason string) (scan.ScanSession, error) {
	return r.TransitionSession(ctx, tx, id, scan.StatusCancelled, reason)
}

// RecordResult는 (session, robot, check) 결과를 INSERT하고 진행률을 갱신합니다.
//
// session.Status != running이면 ErrSessionNotRunning.
// 같은 (session_id, robot_id, check_id) 중복 시 ErrResultDuplicate (UNIQUE 강제).
// outcome ∈ {fail, error}이면 progress.Failed +1, 모든 outcome은 progress.Completed +1.
func (r *Repo) RecordResult(ctx context.Context, tx storage.Tx, req scan.RecordResultRequest) (scan.ScanResult, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return scan.ScanResult{}, storage.ErrTenantMissing
	}
	if err := validateRecordResult(&req); err != nil {
		return scan.ScanResult{}, err
	}

	session, err := r.GetSession(ctx, tx, req.SessionID)
	if err != nil {
		return scan.ScanResult{}, err
	}
	if session.Status != scan.StatusRunning {
		return scan.ScanResult{}, scan.ErrSessionNotRunning
	}

	now := r.deps.Clock.Now().UTC()
	executedAt := req.ExecutedAt
	if executedAt.IsZero() {
		executedAt = now
	}
	result := scan.ScanResult{
		ID:          r.deps.IDGen.New("scr"),
		SessionID:   req.SessionID,
		TenantID:    tenantID,
		RobotID:     req.RobotID,
		CheckID:     req.CheckID,
		PackCheckID: req.PackCheckID,
		Outcome:     req.Outcome,
		EvalReason:  req.EvalReason,
		DurationMs:  req.DurationMs,
		ExecutedAt:  executedAt.UTC(),
		CreatedAt:   now,
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO scan_results (
    id, session_id, tenant_id, robot_id, check_id, pack_check_id,
    outcome, eval_reason, duration_ms,
    executed_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		result.ID, result.SessionID, string(result.TenantID), result.RobotID,
		result.CheckID, result.PackCheckID, string(result.Outcome),
		result.EvalReason, result.DurationMs,
		result.ExecutedAt.Format(rfc3339Nano), result.CreatedAt.Format(rfc3339Nano),
	); err != nil {
		if isUniqueViolation(err) {
			return scan.ScanResult{}, scan.ErrResultDuplicate
		}
		return scan.ScanResult{}, fmt.Errorf("scan: insert result: %w", err)
	}

	// 진행률 갱신 — atomic UPDATE.
	failedDelta := 0
	if req.Outcome == scan.OutcomeFail || req.Outcome == scan.OutcomeError {
		failedDelta = 1
	}
	if _, err := tx.Exec(ctx, `
UPDATE scan_sessions
   SET progress_completed = progress_completed + 1,
       progress_failed    = progress_failed + ?,
       updated_at         = ?
 WHERE id = ? AND tenant_id = ?`,
		failedDelta, now.Format(rfc3339Nano), req.SessionID, string(tenantID),
	); err != nil {
		return scan.ScanResult{}, fmt.Errorf("scan: update progress: %w", err)
	}

	return result, nil
}

// ListResults는 세션의 모든 결과를 created_at ASC로 반환합니다.
func (r *Repo) ListResults(ctx context.Context, tx storage.Tx, sessionID string) ([]scan.ScanResult, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	rows, err := tx.Query(ctx, resultSelectColumns+`
  FROM scan_results
 WHERE session_id = ? AND tenant_id = ?
 ORDER BY created_at ASC`,
		sessionID, string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("scan: list results: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []scan.ScanResult
	for rows.Next() {
		res, err := scanResultRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan: list results iterate: %w", err)
	}
	return out, nil
}

// --- helpers ---

const sessionSelectColumns = `
SELECT id, tenant_id, fleet_id, pack_id, trigger, status,
       progress_total, progress_completed, progress_failed,
       failure_reason, created_at, updated_at, started_at, completed_at`

const resultSelectColumns = `
SELECT id, session_id, tenant_id, robot_id, check_id, pack_check_id,
       outcome, eval_reason, duration_ms,
       executed_at, created_at`

func updateSessionStatus(ctx context.Context, tx storage.Tx, s scan.ScanSession) error {
	var startedAt, completedAt sql.NullString
	if s.StartedAt != nil {
		startedAt = sql.NullString{String: s.StartedAt.Format(rfc3339Nano), Valid: true}
	}
	if s.CompletedAt != nil {
		completedAt = sql.NullString{String: s.CompletedAt.Format(rfc3339Nano), Valid: true}
	}
	_, err := tx.Exec(ctx, `
UPDATE scan_sessions
   SET status         = ?,
       failure_reason = ?,
       updated_at     = ?,
       started_at     = ?,
       completed_at   = ?
 WHERE id = ? AND tenant_id = ?`,
		string(s.Status), s.FailureReason, s.UpdatedAt.Format(rfc3339Nano),
		startedAt, completedAt, s.ID, string(s.TenantID))
	return err
}

func emitTransition(ctx context.Context, tx storage.Tx, em scan.AuditEmitter, from scan.SessionStatus, next scan.ScanSession, reason string) error {
	switch next.Status {
	case scan.StatusRunning:
		if from == scan.StatusPending {
			return em.EmitScanStarted(ctx, tx, next)
		}
	case scan.StatusCompleted:
		return em.EmitScanCompleted(ctx, tx, next)
	case scan.StatusFailed:
		return em.EmitScanFailed(ctx, tx, next, reason)
	case scan.StatusCancelled:
		return em.EmitScanCancelled(ctx, tx, next, reason)
	}
	return nil
}

func scanSessionRow(scanFn func(...any) error) (scan.ScanSession, error) {
	var (
		id, tenantID, fleetID, packID, trigger, status string
		failureReason, createdAt, updatedAt            string
		total, completed, failed                       int
		startedAt, completedAt                         sql.NullString
	)
	if err := scanFn(&id, &tenantID, &fleetID, &packID, &trigger, &status,
		&total, &completed, &failed,
		&failureReason, &createdAt, &updatedAt, &startedAt, &completedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return scan.ScanSession{}, storage.ErrNotFound
		}
		return scan.ScanSession{}, fmt.Errorf("scan: scan session row: %w", err)
	}
	created, err := time.Parse(rfc3339Nano, createdAt)
	if err != nil {
		return scan.ScanSession{}, fmt.Errorf("scan: parse created_at: %w", err)
	}
	updated, err := time.Parse(rfc3339Nano, updatedAt)
	if err != nil {
		return scan.ScanSession{}, fmt.Errorf("scan: parse updated_at: %w", err)
	}
	s := scan.ScanSession{
		ID:            id,
		TenantID:      storage.TenantID(tenantID),
		FleetID:       fleetID,
		PackID:        packID,
		Trigger:       scan.SessionTrigger(trigger),
		Status:        scan.SessionStatus(status),
		Progress:      scan.SessionProgress{Total: total, Completed: completed, Failed: failed},
		FailureReason: failureReason,
		CreatedAt:     created,
		UpdatedAt:     updated,
	}
	if startedAt.Valid {
		t, err := time.Parse(rfc3339Nano, startedAt.String)
		if err != nil {
			return scan.ScanSession{}, fmt.Errorf("scan: parse started_at: %w", err)
		}
		s.StartedAt = &t
	}
	if completedAt.Valid {
		t, err := time.Parse(rfc3339Nano, completedAt.String)
		if err != nil {
			return scan.ScanSession{}, fmt.Errorf("scan: parse completed_at: %w", err)
		}
		s.CompletedAt = &t
	}
	return s, nil
}

func scanResultRow(scanFn func(...any) error) (scan.ScanResult, error) {
	var (
		id, sessionID, tenantID, robotID, checkID, packCheckID string
		outcome, evalReason, executedAt, createdAt             string
		durationMs                                             int64
	)
	if err := scanFn(&id, &sessionID, &tenantID, &robotID, &checkID, &packCheckID,
		&outcome, &evalReason, &durationMs,
		&executedAt, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return scan.ScanResult{}, storage.ErrNotFound
		}
		return scan.ScanResult{}, fmt.Errorf("scan: scan result row: %w", err)
	}
	executed, err := time.Parse(rfc3339Nano, executedAt)
	if err != nil {
		return scan.ScanResult{}, fmt.Errorf("scan: parse executed_at: %w", err)
	}
	created, err := time.Parse(rfc3339Nano, createdAt)
	if err != nil {
		return scan.ScanResult{}, fmt.Errorf("scan: parse result created_at: %w", err)
	}
	return scan.ScanResult{
		ID:          id,
		SessionID:   sessionID,
		TenantID:    storage.TenantID(tenantID),
		RobotID:     robotID,
		CheckID:     checkID,
		PackCheckID: packCheckID,
		Outcome:     scan.Outcome(outcome),
		EvalReason:  evalReason,
		DurationMs:  durationMs,
		ExecutedAt:  executed,
		CreatedAt:   created,
	}, nil
}

func validateStartScan(req *scan.StartScanRequest) error {
	req.FleetID = strings.TrimSpace(req.FleetID)
	req.PackID = strings.TrimSpace(req.PackID)
	if req.FleetID == "" {
		return scan.ErrSessionEmptyFleet
	}
	if req.PackID == "" {
		return scan.ErrSessionEmptyPack
	}
	if req.Trigger == "" {
		req.Trigger = scan.TriggerManual
	}
	if err := scan.ValidateTrigger(req.Trigger); err != nil {
		return err
	}
	if req.Total < 0 {
		return scan.ErrSessionNegativeTotal
	}
	return nil
}

func validateRecordResult(req *scan.RecordResultRequest) error {
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.RobotID = strings.TrimSpace(req.RobotID)
	req.CheckID = strings.TrimSpace(req.CheckID)
	req.PackCheckID = strings.TrimSpace(req.PackCheckID)
	if req.SessionID == "" {
		return storage.ErrNotFound // GetSession에서 잡히지만 빠른 fail.
	}
	if req.RobotID == "" {
		return scan.ErrResultEmptyRobot
	}
	if req.CheckID == "" {
		return scan.ErrResultEmptyCheck
	}
	if req.PackCheckID == "" {
		return scan.ErrResultEmptyPackCheck
	}
	return scan.ValidateOutcome(req.Outcome)
}

func assertFleetActive(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fleetID string) error {
	row := tx.QueryRow(ctx, `
SELECT 1 FROM fleets
 WHERE id = ? AND tenant_id = ? AND deleted_at IS NULL`,
		fleetID, string(tenantID))
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return scan.ErrFleetNotFound
		}
		return fmt.Errorf("scan: lookup fleet: %w", err)
	}
	return nil
}

// assertNoActiveFleetSession은 같은 tenant·fleet에 pending/running 세션이 이미 있으면
// scan.ErrFleetActiveScanExists를 반환합니다 (동시 스캔 limit). 같은 Tx 안에서 실행되어
// SQLite는 직렬 보장. PostgreSQL는 race 가능성 — 별 epic에서 partial unique index 추가.
func assertNoActiveFleetSession(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, fleetID string) error {
	row := tx.QueryRow(ctx, `
SELECT 1 FROM scan_sessions
 WHERE tenant_id = ? AND fleet_id = ? AND status IN ('pending', 'running')
 LIMIT 1`,
		string(tenantID), fleetID)
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("scan: check active session: %w", err)
	}
	return scan.ErrFleetActiveScanExists
}

// assertPackAccessible은 호출 tenant 또는 'system' 소유 팩을 허용합니다 (§4.2 system pack).
func assertPackAccessible(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, packID string) error {
	row := tx.QueryRow(ctx, `
SELECT 1 FROM packs
 WHERE id = ? AND tenant_id IN (?, 'system')`,
		packID, string(tenantID))
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return scan.ErrPackNotFound
		}
		return fmt.Errorf("scan: lookup pack: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
