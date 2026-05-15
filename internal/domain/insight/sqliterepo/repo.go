// Package sqliterepo는 insight.Service의 SQLite 어댑터입니다 (E14 Phase 2).
//
// 책임:
//
//	RunForFleet → ScanReader로 직전 N session 회수 → drift·anomaly·peer detector 실행
//	             → INSERT insights + audit emit (insight.created)
//	             → dedup: 동일 (tenant, kind, scope, summary 첫 50자) 활성 Insight 있으면 skip
//	ListActive → SELECT (filter + DESC, LIMIT, dismissed_at IS NULL)
//	Dismiss    → UPDATE dismissed_at + audit emit
//
// 도메인 결합: bootstrap이 audit/scan 어댑터를 AuditEmitter/ScanReader로 주입.
package sqliterepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/insight"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// rfc3339Nano는 DB 시간 칼럼 직렬화 포맷입니다.
const rfc3339Nano = time.RFC3339Nano

// defaultListLimit는 ListActive의 기본 limit입니다.
const defaultListLimit = 50

// summaryDedupPrefix는 dedup 비교에 사용되는 summary 첫 N자입니다.
const summaryDedupPrefix = 50

// Deps는 어댑터 의존성입니다.
type Deps struct {
	Clock clock.Clock
	IDGen idgen.IDGen
	Audit insight.AuditEmitter // bootstrap에서 audit.Service 어댑터 주입.
	Scan  insight.ScanReader   // bootstrap에서 scan.Service 어댑터 주입.
}

// Repo는 insight.Service의 SQLite 구현입니다.
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// RunForFleet은 fleet 단위 detector 3종(drift·anomaly·peer)을 실행하고 신규 Insight를 INSERT합니다.
//
// 흐름:
//
//  1. ScanReader.ListRecentSessions(fleetID, DefaultDriftWindow) — 직전 N session.
//  2. 각 session에 대해 ScanReader.ListResultsForSession 호출 → resultsBySession.
//  3. drift / anomaly / peer detector 호출 (ErrInsufficientHistory는 빈 결과로 흡수).
//  4. 각 detector 산출 Insight를 dedup 후 INSERT + audit emit.
//
// 빈 history(session 0개) → 빈 결과 반환 (에러 아님).
func (r *Repo) RunForFleet(ctx context.Context, tx storage.Tx, fleetID string) ([]insight.Insight, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	if r.deps.Scan == nil {
		return nil, fmt.Errorf("insight: ScanReader is nil")
	}

	sessions, err := r.deps.Scan.ListRecentSessions(ctx, tx, fleetID, insight.DefaultDriftWindow)
	if err != nil {
		return nil, fmt.Errorf("insight: list sessions: %w", err)
	}
	if len(sessions) == 0 {
		return nil, nil
	}

	resultsBySession := make(map[string][]insight.ScanResultView, len(sessions))
	for _, s := range sessions {
		results, err := r.deps.Scan.ListResultsForSession(ctx, tx, s.ID)
		if err != nil {
			return nil, fmt.Errorf("insight: list results: %w", err)
		}
		resultsBySession[s.ID] = results
	}

	now := r.deps.Clock.Now().UTC()

	var produced []insight.Insight

	driftIns, err := insight.DetectDrift(now, sessions, resultsBySession)
	if err != nil && !errors.Is(err, insight.ErrInsufficientHistory) {
		return nil, fmt.Errorf("insight: drift: %w", err)
	}
	produced = append(produced, driftIns...)

	anomalyIns, err := insight.DetectAnomaly(now, sessions, resultsBySession)
	if err != nil && !errors.Is(err, insight.ErrInsufficientHistory) {
		return nil, fmt.Errorf("insight: anomaly: %w", err)
	}
	produced = append(produced, anomalyIns...)

	peerIns, err := insight.DetectPeer(now, fleetID, sessions, resultsBySession)
	if err != nil {
		return nil, fmt.Errorf("insight: peer: %w", err)
	}
	produced = append(produced, peerIns...)

	// dedup + insert.
	var inserted []insight.Insight
	for _, in := range produced {
		// detector 산출에는 ID 없음 — repo가 부여.
		in.TenantID = tenantID
		in.ID = r.deps.IDGen.New("ins")
		dup, err := r.hasActiveDuplicate(ctx, tx, in)
		if err != nil {
			return nil, fmt.Errorf("insight: dedup check: %w", err)
		}
		if dup {
			continue
		}
		if err := r.insertInsight(ctx, tx, in); err != nil {
			return nil, fmt.Errorf("insight: insert: %w", err)
		}
		if r.deps.Audit != nil {
			if err := r.deps.Audit.EmitInsightCreated(ctx, tx, in); err != nil {
				return nil, fmt.Errorf("insight: emit created: %w", err)
			}
		}
		inserted = append(inserted, in)
	}
	return inserted, nil
}

// ListActive는 dismissed_at=NULL인 Insight를 created_at DESC로 반환합니다.
func (r *Repo) ListActive(ctx context.Context, tx storage.Tx, filter insight.ListFilter) ([]insight.Insight, error) {
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
	query.WriteString(insightSelectColumns)
	query.WriteString(`
  FROM insights
 WHERE tenant_id = ? AND dismissed_at IS NULL`)
	args = append(args, string(tenantID))
	if filter.Kind != "" {
		query.WriteString(` AND kind = ?`)
		args = append(args, string(filter.Kind))
	}
	if filter.Severity != "" {
		query.WriteString(` AND severity = ?`)
		args = append(args, string(filter.Severity))
	}
	if filter.RobotID != "" {
		query.WriteString(` AND scope_robot_id = ?`)
		args = append(args, filter.RobotID)
	}
	query.WriteString(` ORDER BY created_at DESC LIMIT ?`)
	args = append(args, limit)

	rows, err := tx.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("insight: list active: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []insight.Insight
	for rows.Next() {
		in, err := scanInsightRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("insight: list active iterate: %w", err)
	}
	return out, nil
}

// Dismiss는 Insight를 dismissed로 마킹 + audit emit합니다.
//
// 이미 dismissed면 ErrInsightNotFound (활성 인덱스 미스).
func (r *Repo) Dismiss(ctx context.Context, tx storage.Tx, insightID, dismissedBy, reason string) (insight.Insight, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return insight.Insight{}, storage.ErrTenantMissing
	}
	current, err := r.getActive(ctx, tx, insightID)
	if err != nil {
		return insight.Insight{}, err
	}

	now := r.deps.Clock.Now().UTC()
	if _, err := tx.Exec(ctx, `
UPDATE insights
   SET dismissed_at = ?,
       dismissed_by = ?
 WHERE id = ? AND tenant_id = ? AND dismissed_at IS NULL`,
		now.Format(rfc3339Nano), dismissedBy, insightID, string(tenantID),
	); err != nil {
		return insight.Insight{}, fmt.Errorf("insight: update dismissed: %w", err)
	}

	dismissedAt := now
	current.DismissedAt = &dismissedAt
	current.DismissedBy = dismissedBy

	if r.deps.Audit != nil {
		if err := r.deps.Audit.EmitInsightDismissed(ctx, tx, current, reason); err != nil {
			return insight.Insight{}, fmt.Errorf("insight: emit dismissed: %w", err)
		}
	}
	return current, nil
}

// GetInsight는 ID로 Insight 1건을 반환합니다 (RBAC fleet 정밀화 Stage 6).
//
// 활성/비활성 모두 조회 — POST /insights/{id}:dismiss의 fleet scope 정밀 평가가 ScopeResolver
// 단계에서 호출하기 위해 도입. tenant 격리는 storage.Tx의 tenant_id로 자동 — cross-tenant
// lookup은 sql.ErrNoRows → storage.ErrNotFound → ErrInsightNotFound로 차단합니다.
func (r *Repo) GetInsight(ctx context.Context, tx storage.Tx, insightID string) (insight.Insight, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return insight.Insight{}, storage.ErrTenantMissing
	}
	row := tx.QueryRow(ctx, insightSelectColumns+`
  FROM insights
 WHERE id = ? AND tenant_id = ?`,
		insightID, string(tenantID))
	in, err := scanInsightRow(row.Scan)
	if errors.Is(err, storage.ErrNotFound) {
		return insight.Insight{}, insight.ErrInsightNotFound
	}
	return in, err
}

// --- helpers ---

const insightSelectColumns = `
SELECT id, tenant_id, kind, severity,
       scope_robot_id, scope_fleet_id, scope_check_id,
       summary, reasoning, evidence_json, rules_applied,
       confidence, produced_by, created_at, dismissed_at, dismissed_by`

func (r *Repo) insertInsight(ctx context.Context, tx storage.Tx, in insight.Insight) error {
	rulesJSON, err := json.Marshal(in.RulesApplied)
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}
	if len(in.EvidenceJSON) == 0 {
		in.EvidenceJSON = []byte("[]")
	}
	scopeRobot := nullableString(in.Scope.RobotID)
	scopeFleet := nullableString(in.Scope.FleetID)
	scopeCheck := nullableString(in.Scope.CheckID)

	_, err = tx.Exec(ctx, `
INSERT INTO insights (
    id, tenant_id, kind, severity,
    scope_robot_id, scope_fleet_id, scope_check_id,
    summary, reasoning, evidence_json, rules_applied,
    confidence, produced_by, created_at, dismissed_at, dismissed_by
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		in.ID, string(in.TenantID), string(in.Kind), string(in.Severity),
		scopeRobot, scopeFleet, scopeCheck,
		in.Summary, in.Reasoning, string(in.EvidenceJSON), string(rulesJSON),
		in.Confidence, string(in.ProducedBy), in.CreatedAt.Format(rfc3339Nano),
	)
	return err
}

func (r *Repo) hasActiveDuplicate(ctx context.Context, tx storage.Tx, in insight.Insight) (bool, error) {
	prefix := in.Summary
	if len(prefix) > summaryDedupPrefix {
		prefix = prefix[:summaryDedupPrefix]
	}
	scopeRobot := nullableString(in.Scope.RobotID)
	scopeFleet := nullableString(in.Scope.FleetID)
	scopeCheck := nullableString(in.Scope.CheckID)

	row := tx.QueryRow(ctx, `
SELECT 1 FROM insights
 WHERE tenant_id = ? AND kind = ? AND dismissed_at IS NULL
   AND (scope_robot_id IS ? OR (scope_robot_id IS NULL AND ? IS NULL))
   AND (scope_fleet_id IS ? OR (scope_fleet_id IS NULL AND ? IS NULL))
   AND (scope_check_id IS ? OR (scope_check_id IS NULL AND ? IS NULL))
   AND substr(summary, 1, ?) = ?
 LIMIT 1`,
		string(in.TenantID), string(in.Kind),
		scopeRobot, scopeRobot,
		scopeFleet, scopeFleet,
		scopeCheck, scopeCheck,
		summaryDedupPrefix, prefix,
	)
	var dummy int
	err := row.Scan(&dummy)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (r *Repo) getActive(ctx context.Context, tx storage.Tx, insightID string) (insight.Insight, error) {
	tenantID := tx.TenantID()
	row := tx.QueryRow(ctx, insightSelectColumns+`
  FROM insights
 WHERE id = ? AND tenant_id = ? AND dismissed_at IS NULL`,
		insightID, string(tenantID))
	in, err := scanInsightRow(row.Scan)
	if errors.Is(err, storage.ErrNotFound) {
		return insight.Insight{}, insight.ErrInsightNotFound
	}
	return in, err
}

func scanInsightRow(scanFn func(...any) error) (insight.Insight, error) {
	var (
		id, tenantID, kind, severity                        string
		summary, reasoning, evidenceJSON, rulesJSON, prodBy string
		confidence                                          float64
		createdAt                                           string
		scopeRobot, scopeFleet, scopeCheck                  sql.NullString
		dismissedAt, dismissedBy                            sql.NullString
	)
	if err := scanFn(&id, &tenantID, &kind, &severity,
		&scopeRobot, &scopeFleet, &scopeCheck,
		&summary, &reasoning, &evidenceJSON, &rulesJSON,
		&confidence, &prodBy, &createdAt, &dismissedAt, &dismissedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return insight.Insight{}, storage.ErrNotFound
		}
		return insight.Insight{}, fmt.Errorf("insight: scan row: %w", err)
	}
	created, err := time.Parse(rfc3339Nano, createdAt)
	if err != nil {
		return insight.Insight{}, fmt.Errorf("insight: parse created_at: %w", err)
	}
	in := insight.Insight{
		ID:           id,
		TenantID:     storage.TenantID(tenantID),
		Kind:         insight.Kind(kind),
		Severity:     insight.Severity(severity),
		Scope:        insight.Scope{RobotID: scopeRobot.String, FleetID: scopeFleet.String, CheckID: scopeCheck.String},
		Summary:      summary,
		Reasoning:    reasoning,
		EvidenceJSON: []byte(evidenceJSON),
		Confidence:   confidence,
		ProducedBy:   insight.ProducedBy(prodBy),
		CreatedAt:    created,
	}
	if rulesJSON != "" {
		var rules []string
		if err := json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
			return insight.Insight{}, fmt.Errorf("insight: unmarshal rules: %w", err)
		}
		in.RulesApplied = rules
	}
	if dismissedAt.Valid {
		t, err := time.Parse(rfc3339Nano, dismissedAt.String)
		if err != nil {
			return insight.Insight{}, fmt.Errorf("insight: parse dismissed_at: %w", err)
		}
		in.DismissedAt = &t
	}
	if dismissedBy.Valid {
		in.DismissedBy = dismissedBy.String
	}
	return in, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
