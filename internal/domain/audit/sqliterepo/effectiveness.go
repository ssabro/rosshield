package sqliterepo

// effectiveness.go — Phase 11.B-6 audit event aggregation for SOC2 effectiveness
// dashboard.
//
// audit_entries 를 (action, time-range) 별로 한번에 집계하는 read-only query.
// design doc: docs/design/notes/soc2-readiness-design.md §7.6 (Stage 11.B-6).
//
// 본 파일은 audit.EffectivenessAggregator interface 의 SQLite/Postgres 어댑터
// 구현입니다 — handler 는 본 메서드를 통해 통제 effectiveness 매트릭스의 행별
// audit event 카운트를 회수합니다.
//
// 성능: actions IN (...) + occurred_at >= ? 조건. audit_entries 의 PRIMARY KEY
// (tenant_id, seq) + audit_entries_tenant_occurred (tenant_id, occurred_at) 인덱스
// 활용. 매핑 대상 action 수가 ~50 이하 + 30 일 윈도우면 1 query 로 충분.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// CountActionsByWindows 는 audit.EffectivenessAggregator 구현입니다.
//
// 3 윈도우(1d/7d/30d)를 단일 query 로 집계 — SUM(CASE WHEN ...) 트릭으로
// occurred_at 비교를 컬럼별로 분기. actions 슬라이스 순서 그대로 반환되도록
// caller 가 IN 절 결과를 dedup map 으로 재정렬.
//
// actions 가 empty 면 빈 결과. context cancel 또는 query 에러는 그대로 반환.
func (r *Repo) CountActionsByWindows(
	ctx context.Context,
	tx storage.Tx,
	tenantID storage.TenantID,
	actions []string,
	now time.Time,
) ([]audit.ActionCountWindow, error) {
	if tx.TenantID() != "" && tx.TenantID() != tenantID {
		return nil, errors.New("audit/effectiveness: tx tenant mismatch")
	}
	out := make([]audit.ActionCountWindow, len(actions))
	for i, a := range actions {
		out[i] = audit.ActionCountWindow{Action: a}
	}
	if len(actions) == 0 {
		return out, nil
	}

	now = now.UTC()
	t1 := now.Add(-24 * time.Hour).Format(time.RFC3339Nano)
	t7 := now.Add(-7 * 24 * time.Hour).Format(time.RFC3339Nano)
	t30 := now.Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)

	// IN 절 placeholders 생성. SQLite native = `?`, Postgres adapter (pg.go::rebind) 가
	// `?` → `$1, $2, ...` 로 자동 변환 — 본 query 는 항상 `?` 사용.
	// 시간 placeholder 3 개 + tenant placeholder 1 개 + actions N 개.
	args := make([]any, 0, 4+len(actions))
	args = append(args, t1, t7, t30, string(tenantID))
	inParts := make([]string, len(actions))
	for i, a := range actions {
		inParts[i] = "?"
		args = append(args, a)
	}

	query := `
SELECT
    action,
    SUM(CASE WHEN occurred_at >= ? THEN 1 ELSE 0 END) AS c1,
    SUM(CASE WHEN occurred_at >= ? THEN 1 ELSE 0 END) AS c7,
    SUM(CASE WHEN occurred_at >= ? THEN 1 ELSE 0 END) AS c30
FROM audit_entries
WHERE tenant_id = ?
  AND action IN (` + strings.Join(inParts, ", ") + `)
GROUP BY action`

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit/effectiveness: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	idxByAction := make(map[string]int, len(actions))
	for i, a := range actions {
		idxByAction[a] = i
	}

	for rows.Next() {
		var (
			action     string
			lastDay    int64
			last7Days  int64
			last30Days int64
		)
		if err := rows.Scan(&action, &lastDay, &last7Days, &last30Days); err != nil {
			return nil, fmt.Errorf("audit/effectiveness: scan: %w", err)
		}
		i, ok := idxByAction[action]
		if !ok {
			continue
		}
		out[i].LastDay = lastDay
		out[i].Last7Days = last7Days
		out[i].Last30Days = last30Days
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit/effectiveness: rows: %w", err)
	}
	return out, nil
}
