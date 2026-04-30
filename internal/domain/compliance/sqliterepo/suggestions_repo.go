// suggestions_repo.go — E17 Phase 2 LLM 자동 매핑 제안 영속.
//
// 흐름 (SuggestMappings):
//
//	candidate controls = LoadFramework(req.Framework)에서 추출 (TopN 제한은 LLMSuggester가 처리)
//	→ Suggester.Suggest(req+candidates) → []SuggestionDraft
//	→ 각 draft를 mapping_suggestions INSERT (UNIQUE 충돌은 silently skip — dedup)
//	→ INSERT 성공한 것만 audit emit + 반환.
//
// 흐름 (Confirm/Reject):
//
//	GetSuggestion → status=pending 강제 → UPDATE status·decided_at·decided_by + audit emit.

package sqliterepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/compliance"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// SuggestMappings는 LLMSuggester를 호출해 후보 control을 받아 mapping_suggestions에 INSERT합니다.
//
// LLM이 ErrLLMDisabled를 반환하면 ErrLLMSuggesterUnavailable 래핑.
// (tenant, check_code, control_id) UNIQUE 충돌은 무시하고 다음으로 진행 (이미 제안됨).
func (r *Repo) SuggestMappings(ctx context.Context, tx storage.Tx, req compliance.SuggestMappingsRequest) ([]compliance.MappingSuggestion, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	if r.deps.Suggester == nil {
		return nil, compliance.ErrLLMSuggesterUnavailable
	}
	if err := compliance.ValidateFramework(req.Framework); err != nil {
		return nil, err
	}
	checkCode := strings.TrimSpace(req.CheckCode)
	if checkCode == "" {
		return nil, fmt.Errorf("compliance: check code is required")
	}

	// 1) candidate controls — embed YAML에서 추출.
	defs, _, err := compliance.LoadFramework(req.Framework)
	if err != nil {
		return nil, fmt.Errorf("compliance: load framework: %w", err)
	}
	candidates := make([]compliance.CandidateControl, 0, len(defs))
	for _, d := range defs {
		candidates = append(candidates, compliance.CandidateControl{
			ID:      d.ID,
			Title:   d.Title,
			Summary: d.Summary,
		})
	}

	// 2) LLM 호출.
	resp, err := r.deps.Suggester.Suggest(ctx, compliance.SuggestRequest{
		CheckCode:         checkCode,
		CheckTitle:        req.CheckTitle,
		CheckRationale:    req.CheckRationale,
		Framework:         req.Framework,
		CandidateControls: candidates,
		TopN:              req.TopN,
	})
	if err != nil {
		// LLM 비활성/타임아웃은 caller가 fallback 결정 — 도메인 sentinel로 normalize.
		return nil, fmt.Errorf("%w: %v", compliance.ErrLLMSuggesterUnavailable, err)
	}

	// 3) 각 draft INSERT — UNIQUE 충돌은 silently skip.
	now := r.deps.Clock.Now().UTC()
	out := make([]compliance.MappingSuggestion, 0, len(resp.Suggestions))
	for _, d := range resp.Suggestions {
		s := compliance.MappingSuggestion{
			ID:          r.deps.IDGen.New("ms"),
			TenantID:    tenantID,
			CheckCode:   checkCode,
			Framework:   req.Framework,
			ControlID:   d.ControlID,
			Confidence:  d.Confidence,
			Reasoning:   d.Reasoning,
			ProducedBy:  compliance.SuggestionByLLM,
			Status:      compliance.SuggestionPending,
			LLMProvider: resp.LLMProvider,
			LLMModel:    resp.LLMModel,
			CreatedAt:   now,
		}
		ins, inserted, err := r.insertSuggestion(ctx, tx, s)
		if err != nil {
			return nil, err
		}
		if !inserted {
			continue // UNIQUE 충돌 — 이미 제안됨, skip.
		}
		if err := r.deps.Audit.EmitSuggestionCreated(ctx, tx, ins); err != nil {
			return nil, fmt.Errorf("compliance: emit suggestion.created: %w", err)
		}
		out = append(out, ins)
	}
	return out, nil
}

// insertSuggestion은 단일 INSERT를 시도합니다. UNIQUE 충돌은 (zero, false, nil) 반환.
func (r *Repo) insertSuggestion(ctx context.Context, tx storage.Tx, s compliance.MappingSuggestion) (compliance.MappingSuggestion, bool, error) {
	_, err := tx.Exec(ctx, `INSERT INTO mapping_suggestions (
    id, tenant_id, check_code, framework, control_id,
    confidence, reasoning, produced_by, status,
    llm_provider, llm_model, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, string(s.TenantID), s.CheckCode, string(s.Framework), s.ControlID,
		s.Confidence, s.Reasoning, string(s.ProducedBy), string(s.Status),
		s.LLMProvider, s.LLMModel, s.CreatedAt.Format(rfc3339Nano),
	)
	if err != nil {
		// SQLite UNIQUE 위반은 driver별 메시지가 다르나 modernc.org/sqlite는 "UNIQUE constraint failed" 포함.
		if isUniqueConflict(err) {
			return compliance.MappingSuggestion{}, false, nil
		}
		return compliance.MappingSuggestion{}, false, fmt.Errorf("compliance: insert suggestion: %w", err)
	}
	return s, true, nil
}

// isUniqueConflict는 INSERT가 UNIQUE 위반인지 판정합니다 (modernc.org/sqlite 메시지 기반).
func isUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// ListSuggestions는 filter 기준으로 제안 목록을 created_at DESC로 반환합니다.
func (r *Repo) ListSuggestions(ctx context.Context, tx storage.Tx, filter compliance.SuggestionListFilter) ([]compliance.MappingSuggestion, error) {
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
	query.WriteString(`SELECT id, tenant_id, check_code, framework, control_id,
       confidence, reasoning, produced_by, status,
       llm_provider, llm_model, created_at, decided_at, decided_by
FROM mapping_suggestions
WHERE tenant_id = ?`)
	args = append(args, string(tenantID))
	if cc := strings.TrimSpace(filter.CheckCode); cc != "" {
		query.WriteString(` AND check_code = ?`)
		args = append(args, cc)
	}
	if filter.Framework != "" {
		query.WriteString(` AND framework = ?`)
		args = append(args, string(filter.Framework))
	}
	if filter.Status != "" {
		query.WriteString(` AND status = ?`)
		args = append(args, string(filter.Status))
	}
	query.WriteString(` ORDER BY created_at DESC LIMIT ?`)
	args = append(args, limit)

	rows, err := tx.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("compliance: list suggestions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []compliance.MappingSuggestion
	for rows.Next() {
		s, err := scanSuggestion(rows)
		if err != nil {
			return nil, fmt.Errorf("compliance: scan suggestion: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("compliance: rows err: %w", err)
	}
	return out, nil
}

// ConfirmSuggestion은 pending 제안을 confirmed로 전이합니다.
func (r *Repo) ConfirmSuggestion(ctx context.Context, tx storage.Tx, id, decidedBy string) (compliance.MappingSuggestion, error) {
	return r.decideSuggestion(ctx, tx, id, decidedBy, compliance.SuggestionConfirmed)
}

// RejectSuggestion은 pending 제안을 rejected로 전이합니다.
func (r *Repo) RejectSuggestion(ctx context.Context, tx storage.Tx, id, decidedBy string) (compliance.MappingSuggestion, error) {
	return r.decideSuggestion(ctx, tx, id, decidedBy, compliance.SuggestionRejected)
}

func (r *Repo) decideSuggestion(ctx context.Context, tx storage.Tx, id, decidedBy string, target compliance.SuggestionStatus) (compliance.MappingSuggestion, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return compliance.MappingSuggestion{}, storage.ErrTenantMissing
	}
	current, err := r.getSuggestion(ctx, tx, id)
	if err != nil {
		return compliance.MappingSuggestion{}, err
	}
	if current.TenantID != tenantID {
		// cross-tenant — 격리.
		return compliance.MappingSuggestion{}, compliance.ErrSuggestionNotFound
	}
	if current.Status != compliance.SuggestionPending {
		return compliance.MappingSuggestion{}, compliance.ErrSuggestionAlreadyDecided
	}

	now := r.deps.Clock.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE mapping_suggestions
SET status = ?, decided_at = ?, decided_by = ?
WHERE id = ? AND tenant_id = ?`,
		string(target), now.Format(rfc3339Nano), decidedBy,
		id, string(tenantID),
	); err != nil {
		return compliance.MappingSuggestion{}, fmt.Errorf("compliance: update suggestion: %w", err)
	}

	current.Status = target
	current.DecidedAt = &now
	current.DecidedBy = decidedBy
	if err := r.deps.Audit.EmitSuggestionDecided(ctx, tx, current); err != nil {
		return compliance.MappingSuggestion{}, fmt.Errorf("compliance: emit suggestion.decided: %w", err)
	}
	return current, nil
}

func (r *Repo) getSuggestion(ctx context.Context, tx storage.Tx, id string) (compliance.MappingSuggestion, error) {
	row := tx.QueryRow(ctx, `SELECT id, tenant_id, check_code, framework, control_id,
       confidence, reasoning, produced_by, status,
       llm_provider, llm_model, created_at, decided_at, decided_by
FROM mapping_suggestions
WHERE id = ?`, id)
	s, err := scanSuggestionRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return compliance.MappingSuggestion{}, compliance.ErrSuggestionNotFound
		}
		return compliance.MappingSuggestion{}, fmt.Errorf("compliance: get suggestion: %w", err)
	}
	return s, nil
}

func scanSuggestion(rows *sql.Rows) (compliance.MappingSuggestion, error) {
	return scanSuggestionRow(rows)
}

func scanSuggestionRow(row interface {
	Scan(...any) error
}) (compliance.MappingSuggestion, error) {
	var (
		id, tenantID, checkCode, framework, controlID        string
		reasoning, producedBy, status, llmProvider, llmModel string
		createdAtStr                                         string
		decidedAtStr, decidedBy                              sql.NullString
		confidence                                           float64
	)
	if err := row.Scan(&id, &tenantID, &checkCode, &framework, &controlID,
		&confidence, &reasoning, &producedBy, &status,
		&llmProvider, &llmModel, &createdAtStr, &decidedAtStr, &decidedBy,
	); err != nil {
		return compliance.MappingSuggestion{}, err
	}
	createdAt, err := time.Parse(rfc3339Nano, createdAtStr)
	if err != nil {
		return compliance.MappingSuggestion{}, fmt.Errorf("parse created_at: %w", err)
	}
	out := compliance.MappingSuggestion{
		ID:          id,
		TenantID:    storage.TenantID(tenantID),
		CheckCode:   checkCode,
		Framework:   compliance.Framework(framework),
		ControlID:   controlID,
		Confidence:  confidence,
		Reasoning:   reasoning,
		ProducedBy:  compliance.SuggestionProducedBy(producedBy),
		Status:      compliance.SuggestionStatus(status),
		LLMProvider: llmProvider,
		LLMModel:    llmModel,
		CreatedAt:   createdAt,
		DecidedBy:   decidedBy.String,
	}
	if decidedAtStr.Valid && decidedAtStr.String != "" {
		t, err := time.Parse(rfc3339Nano, decidedAtStr.String)
		if err != nil {
			return compliance.MappingSuggestion{}, fmt.Errorf("parse decided_at: %w", err)
		}
		out.DecidedAt = &t
	}
	return out, nil
}
