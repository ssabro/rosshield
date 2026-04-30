// Package sqliterepo는 advisor.Service의 SQLite 어댑터입니다 (E16-A).
//
// 책임:
//
//	StartConversation       → advisor_conversations INSERT + audit emit
//	AppendTurn              → advisor_turns INSERT (ToolCalls면 advisor_tool_calls 일괄 INSERT)
//	GetConversation         → SELECT conversation + turns(seq ASC) + tool_calls
//	ListConversations       → SELECT tenant·user 스코프, updated_at DESC
//
// 도메인 결합 (P5):
//
//	Ask 흐름은 별도 application service(또는 orchestrator.go E16-C)가 담당.
//	본 패키지는 영속만 — LLM/Tool 호출은 호출자 책임.
package sqliterepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ssabro/rosshield/internal/domain/advisor"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

const rfc3339Nano = time.RFC3339Nano
const defaultListLimit = 50

// Deps는 어댑터 의존성입니다.
type Deps struct {
	Clock clock.Clock
	IDGen idgen.IDGen
	Audit advisor.AuditEmitter
}

// Repo는 영속 + audit emit만 담당합니다.
//
// Ask 흐름(LLM 호출 + tool loop)은 별도 application service(E16-C orchestrator)가 처리.
// 본 Repo는 advisor.Service를 일부만 구현 — Ask는 orchestrator에 위임.
type Repo struct {
	deps Deps
}

// New는 새 Repo를 반환합니다.
func New(deps Deps) *Repo {
	return &Repo{deps: deps}
}

// StartConversation은 새 conversation을 INSERT하고 audit emit합니다.
//
// title은 첫 user question에서 자동 생성 (advisor.MakeTitle).
func (r *Repo) StartConversation(ctx context.Context, tx storage.Tx, userID, question string) (advisor.Conversation, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return advisor.Conversation{}, storage.ErrTenantMissing
	}
	if strings.TrimSpace(userID) == "" {
		return advisor.Conversation{}, fmt.Errorf("advisor: userID is required")
	}
	now := r.deps.Clock.Now().UTC()
	conv := advisor.Conversation{
		ID:        r.deps.IDGen.New("conv"),
		TenantID:  tenantID,
		UserID:    userID,
		Title:     advisor.MakeTitle(strings.TrimSpace(question)),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := tx.Exec(ctx, `INSERT INTO advisor_conversations (id, tenant_id, user_id, title, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)`,
		conv.ID, string(conv.TenantID), conv.UserID, conv.Title,
		conv.CreatedAt.Format(rfc3339Nano), conv.UpdatedAt.Format(rfc3339Nano),
	); err != nil {
		return advisor.Conversation{}, fmt.Errorf("advisor: insert conversation: %w", err)
	}
	if err := r.deps.Audit.EmitConversationStarted(ctx, tx, conv); err != nil {
		return advisor.Conversation{}, fmt.Errorf("advisor: emit conversation.started: %w", err)
	}
	return conv, nil
}

// AppendTurn은 conversation에 새 turn을 INSERT합니다 (sequence 자동 채움).
//
// ToolCalls가 채워져 있으면 advisor_tool_calls도 일괄 INSERT.
// assistant role + ToolCalls가 채워져 있으면 각 ToolCall에 audit emit.
// assistant role + 본문 있으면 advisor.responded audit emit (orchestrator 흐름의 마지막 turn).
//
// conversation.updated_at 갱신.
func (r *Repo) AppendTurn(ctx context.Context, tx storage.Tx, conversationID string, turn advisor.Turn) (advisor.Turn, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return advisor.Turn{}, storage.ErrTenantMissing
	}

	// conversation 조회 + tenant scope 확인.
	var convTenant string
	if err := tx.QueryRow(ctx, `SELECT tenant_id FROM advisor_conversations WHERE id = ?`, conversationID).Scan(&convTenant); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return advisor.Turn{}, advisor.ErrConversationNotFound
		}
		return advisor.Turn{}, fmt.Errorf("advisor: get conversation: %w", err)
	}
	if storage.TenantID(convTenant) != tenantID {
		return advisor.Turn{}, advisor.ErrConversationNotFound // cross-tenant 격리
	}

	// sequence 결정 — MAX(sequence) + 1.
	var maxSeq sql.NullInt64
	if err := tx.QueryRow(ctx, `SELECT MAX(sequence) FROM advisor_turns WHERE conversation_id = ?`, conversationID).Scan(&maxSeq); err != nil {
		return advisor.Turn{}, fmt.Errorf("advisor: max sequence: %w", err)
	}
	seq := 0
	if maxSeq.Valid {
		seq = int(maxSeq.Int64) + 1
	}

	now := r.deps.Clock.Now().UTC()
	turn.ID = r.deps.IDGen.New("turn")
	turn.ConversationID = conversationID
	turn.TenantID = tenantID
	turn.Sequence = seq
	turn.CreatedAt = now

	if _, err := tx.Exec(ctx, `INSERT INTO advisor_turns (
    id, conversation_id, tenant_id, role, content, sequence,
    llm_provider, llm_model, input_tokens, output_tokens, cost_usd, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		turn.ID, turn.ConversationID, string(turn.TenantID), string(turn.Role), turn.Content, turn.Sequence,
		turn.LLMProvider, turn.LLMModel, turn.InputTokens, turn.OutputTokens, turn.CostUSD,
		turn.CreatedAt.Format(rfc3339Nano),
	); err != nil {
		return advisor.Turn{}, fmt.Errorf("advisor: insert turn: %w", err)
	}

	// tool_calls 영속 + audit emit.
	for i := range turn.ToolCalls {
		tc := &turn.ToolCalls[i]
		tc.ID = r.deps.IDGen.New("tcall")
		tc.TurnID = turn.ID
		tc.TenantID = tenantID
		tc.CreatedAt = now
		args := tc.ArgsJSON
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		result := tc.ResultJSON
		if len(result) == 0 {
			result = json.RawMessage("{}")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO advisor_tool_calls (
    id, turn_id, tenant_id, tool_name, args_json, result_json, error, duration_ms, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			tc.ID, tc.TurnID, string(tc.TenantID), tc.ToolName, string(args), string(result),
			tc.Error, tc.DurationMs, tc.CreatedAt.Format(rfc3339Nano),
		); err != nil {
			return advisor.Turn{}, fmt.Errorf("advisor: insert tool_call: %w", err)
		}
		if err := r.deps.Audit.EmitToolCalled(ctx, tx, *tc); err != nil {
			return advisor.Turn{}, fmt.Errorf("advisor: emit tool_called: %w", err)
		}
	}

	// updated_at 갱신.
	if _, err := tx.Exec(ctx, `UPDATE advisor_conversations SET updated_at = ? WHERE id = ?`,
		now.Format(rfc3339Nano), conversationID,
	); err != nil {
		return advisor.Turn{}, fmt.Errorf("advisor: update conversation: %w", err)
	}

	// assistant role + 본문 있으면 advisor.responded emit (최종 답변 시점).
	if turn.Role == advisor.RoleAssistant && strings.TrimSpace(turn.Content) != "" {
		if err := r.deps.Audit.EmitAdvisorResponded(ctx, tx, turn); err != nil {
			return advisor.Turn{}, fmt.Errorf("advisor: emit responded: %w", err)
		}
	}
	return turn, nil
}

// GetConversation은 conversation + 모든 turn(sequence ASC) + tool_calls를 반환합니다.
func (r *Repo) GetConversation(ctx context.Context, tx storage.Tx, conversationID string) (advisor.Conversation, []advisor.Turn, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return advisor.Conversation{}, nil, storage.ErrTenantMissing
	}
	conv, err := r.getConversation(ctx, tx, conversationID, tenantID)
	if err != nil {
		return advisor.Conversation{}, nil, err
	}
	turns, err := r.listTurns(ctx, tx, conversationID)
	if err != nil {
		return advisor.Conversation{}, nil, err
	}
	return conv, turns, nil
}

// ListConversations는 (tenant, user) 스코프 conversation을 updated_at DESC로 반환합니다.
func (r *Repo) ListConversations(ctx context.Context, tx storage.Tx, userID string, limit int) ([]advisor.Conversation, error) {
	tenantID := tx.TenantID()
	if tenantID == "" {
		return nil, storage.ErrTenantMissing
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, user_id, title, created_at, updated_at
FROM advisor_conversations
WHERE tenant_id = ? AND user_id = ?
ORDER BY updated_at DESC LIMIT ?`,
		string(tenantID), userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("advisor: list conversations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []advisor.Conversation
	for rows.Next() {
		var (
			id, tid, uid, title    string
			createdStr, updatedStr string
		)
		if err := rows.Scan(&id, &tid, &uid, &title, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("advisor: scan conversation: %w", err)
		}
		createdAt, _ := time.Parse(rfc3339Nano, createdStr)
		updatedAt, _ := time.Parse(rfc3339Nano, updatedStr)
		out = append(out, advisor.Conversation{
			ID:        id,
			TenantID:  storage.TenantID(tid),
			UserID:    uid,
			Title:     title,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		})
	}
	return out, rows.Err()
}

func (r *Repo) getConversation(ctx context.Context, tx storage.Tx, id string, tenantID storage.TenantID) (advisor.Conversation, error) {
	row := tx.QueryRow(ctx, `SELECT id, tenant_id, user_id, title, created_at, updated_at
FROM advisor_conversations WHERE id = ?`, id)
	var (
		convID, tid, uid, title string
		createdStr, updatedStr  string
	)
	if err := row.Scan(&convID, &tid, &uid, &title, &createdStr, &updatedStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return advisor.Conversation{}, advisor.ErrConversationNotFound
		}
		return advisor.Conversation{}, fmt.Errorf("advisor: get conversation: %w", err)
	}
	if storage.TenantID(tid) != tenantID {
		return advisor.Conversation{}, advisor.ErrConversationNotFound // cross-tenant 격리
	}
	createdAt, _ := time.Parse(rfc3339Nano, createdStr)
	updatedAt, _ := time.Parse(rfc3339Nano, updatedStr)
	return advisor.Conversation{
		ID:        convID,
		TenantID:  storage.TenantID(tid),
		UserID:    uid,
		Title:     title,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func (r *Repo) listTurns(ctx context.Context, tx storage.Tx, conversationID string) ([]advisor.Turn, error) {
	rows, err := tx.Query(ctx, `SELECT id, conversation_id, tenant_id, role, content, sequence,
       llm_provider, llm_model, input_tokens, output_tokens, cost_usd, created_at
FROM advisor_turns
WHERE conversation_id = ?
ORDER BY sequence ASC`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("advisor: list turns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var turns []advisor.Turn
	for rows.Next() {
		var (
			id, convID, tid, role, content, llmP, llmM string
			seq                                        int
			inTok, outTok                              int
			cost                                       float64
			createdStr                                 string
		)
		if err := rows.Scan(&id, &convID, &tid, &role, &content, &seq,
			&llmP, &llmM, &inTok, &outTok, &cost, &createdStr,
		); err != nil {
			return nil, fmt.Errorf("advisor: scan turn: %w", err)
		}
		createdAt, _ := time.Parse(rfc3339Nano, createdStr)
		turns = append(turns, advisor.Turn{
			ID:             id,
			ConversationID: convID,
			TenantID:       storage.TenantID(tid),
			Role:           advisor.Role(role),
			Content:        content,
			Sequence:       seq,
			LLMProvider:    llmP,
			LLMModel:       llmM,
			InputTokens:    inTok,
			OutputTokens:   outTok,
			CostUSD:        cost,
			CreatedAt:      createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 각 turn의 tool_calls 일괄 회수 (N+1 회피 — turn 적으니 단순화).
	for i := range turns {
		tcs, err := r.listToolCalls(ctx, tx, turns[i].ID)
		if err != nil {
			return nil, err
		}
		turns[i].ToolCalls = tcs
	}
	return turns, nil
}

func (r *Repo) listToolCalls(ctx context.Context, tx storage.Tx, turnID string) ([]advisor.ToolCall, error) {
	rows, err := tx.Query(ctx, `SELECT id, turn_id, tenant_id, tool_name, args_json, result_json, error, duration_ms, created_at
FROM advisor_tool_calls
WHERE turn_id = ?
ORDER BY created_at ASC`, turnID)
	if err != nil {
		return nil, fmt.Errorf("advisor: list tool_calls: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []advisor.ToolCall
	for rows.Next() {
		var (
			id, tid, tname, errStr string
			turnIDStr              string
			argsStr, resultStr     string
			durMs                  int64
			createdStr             string
		)
		if err := rows.Scan(&id, &turnIDStr, &tid, &tname, &argsStr, &resultStr, &errStr, &durMs, &createdStr); err != nil {
			return nil, fmt.Errorf("advisor: scan tool_call: %w", err)
		}
		createdAt, _ := time.Parse(rfc3339Nano, createdStr)
		out = append(out, advisor.ToolCall{
			ID:         id,
			TurnID:     turnIDStr,
			TenantID:   storage.TenantID(tid),
			ToolName:   tname,
			ArgsJSON:   json.RawMessage(argsStr),
			ResultJSON: json.RawMessage(resultStr),
			Error:      errStr,
			DurationMs: durMs,
			CreatedAt:  createdAt,
		})
	}
	return out, rows.Err()
}
