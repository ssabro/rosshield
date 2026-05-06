package handlers

// advisor.go — Advisor 도메인 HTTP 표면 (E19-3, E16 백엔드 노출).
//
// 엔드포인트 3종:
//
//	POST /api/v1/advisor/conversations:ask                  → AskAdvisor
//	GET  /api/v1/advisor/conversations                      → ListAdvisorConversations
//	GET  /api/v1/advisor/conversations/{conversationId}     → GetAdvisorConversation
//
// 옵트인 (P2/R14-1):
//
//	LLM 어댑터가 noop이면 Orchestrator.Ask가 ErrAdvisorDisabled 반환 → 503 Service Unavailable.
//	관리자 안내 — `--llm-provider=ollama` 또는 `=anthropic` 활성화 후 가능.
//
// chi 직접 mount 방식 — openapi spec(advisor 표면)은 후속 정리 (SESSION_HANDOFF 메모).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/ssabro/rosshield/internal/domain/advisor"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// askRequest는 POST .../conversations:ask body입니다.
type askRequest struct {
	ConversationID string `json:"conversationId,omitempty"`
	Question       string `json:"question"`
	MaxToolCalls   int    `json:"maxToolCalls,omitempty"`
}

type toolCallResponse struct {
	ID         string `json:"id"`
	ToolName   string `json:"toolName"`
	ArgsJSON   string `json:"argsJson,omitempty"`
	ResultJSON string `json:"resultJson,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"durationMs"`
}

type turnResponse struct {
	ID             string             `json:"id"`
	ConversationID string             `json:"conversationId"`
	Role           string             `json:"role"`
	Content        string             `json:"content"`
	Sequence       int                `json:"sequence"`
	LLMProvider    string             `json:"llmProvider,omitempty"`
	LLMModel       string             `json:"llmModel,omitempty"`
	InputTokens    int                `json:"inputTokens,omitempty"`
	OutputTokens   int                `json:"outputTokens,omitempty"`
	CostUSD        float64            `json:"costUsd,omitempty"`
	CreatedAt      string             `json:"createdAt"`
	ToolCalls      []toolCallResponse `json:"toolCalls,omitempty"`
}

type askResponse struct {
	ConversationID string         `json:"conversationId"`
	FinalAnswer    string         `json:"finalAnswer"`
	Turns          []turnResponse `json:"turns"`
}

type conversationResponse struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenantId"`
	UserID    string `json:"userId"`
	Title     string `json:"title"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type listConversationsResponse struct {
	Conversations []conversationResponse `json:"conversations"`
}

type getConversationResponse struct {
	Conversation conversationResponse `json:"conversation"`
	Turns        []turnResponse       `json:"turns"`
}

// AskAdvisor는 POST /api/v1/advisor/conversations:ask 핸들러입니다.
//
// LLM 옵트인 disabled 시 503. ConversationID가 비면 신규, 채워지면 기존에 turn 추가.
func (h *Handlers) AskAdvisor(w http.ResponseWriter, r *http.Request) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Advisor == nil {
		writeError(w, http.StatusServiceUnavailable, "advisor: service not configured")
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok || claims.Subject == "" {
		writeError(w, http.StatusUnauthorized, "no user in context")
		return
	}
	userID := claims.Subject

	var body askRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	req := advisor.AskRequest{
		ConversationID: body.ConversationID,
		UserID:         userID,
		Question:       body.Question,
		MaxToolCalls:   body.MaxToolCalls,
	}

	var resp advisor.AskResponse
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Advisor.Ask(ctx, tx, req)
		if e != nil {
			return e
		}
		resp = out
		return nil
	})
	if err != nil {
		writeError(w, advisorErrorStatus(err), err.Error())
		return
	}

	out := askResponse{
		ConversationID: resp.ConversationID,
		FinalAnswer:    resp.FinalAnswer,
		Turns:          mapTurns(resp.Turns),
	}
	writeJSON(w, http.StatusOK, out)
}

// ListAdvisorConversations는 GET /api/v1/advisor/conversations 핸들러입니다.
//
// query: limit (옵션, default 50). 본 user의 conversation만 반환 (tenant·user scope).
func (h *Handlers) ListAdvisorConversations(w http.ResponseWriter, r *http.Request) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Advisor == nil {
		writeError(w, http.StatusServiceUnavailable, "advisor: service not configured")
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok || claims.Subject == "" {
		writeError(w, http.StatusUnauthorized, "no user in context")
		return
	}
	userID := claims.Subject

	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	var convs []advisor.Conversation
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		out, e := h.deps.Advisor.ListConversations(ctx, tx, userID, limit)
		if e != nil {
			return e
		}
		convs = out
		return nil
	})
	if err != nil {
		writeError(w, advisorErrorStatus(err), "list conversations failed")
		return
	}

	out := listConversationsResponse{
		Conversations: make([]conversationResponse, 0, len(convs)),
	}
	for _, c := range convs {
		out.Conversations = append(out.Conversations, mapConversation(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetAdvisorConversation은 GET /api/v1/advisor/conversations/{id} 핸들러입니다.
//
// conversation 메타 + 모든 turn(seq ASC)을 반환.
func (h *Handlers) GetAdvisorConversation(w http.ResponseWriter, r *http.Request, conversationID string) {
	if storage.TenantIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}
	if h.deps.Advisor == nil {
		writeError(w, http.StatusServiceUnavailable, "advisor: service not configured")
		return
	}

	var conv advisor.Conversation
	var turns []advisor.Turn
	err := h.deps.Storage.Tx(r.Context(), func(ctx context.Context, tx storage.Tx) error {
		c, t, e := h.deps.Advisor.GetConversation(ctx, tx, conversationID)
		if e != nil {
			return e
		}
		conv = c
		turns = t
		return nil
	})
	if err != nil {
		writeError(w, advisorErrorStatus(err), "get conversation failed")
		return
	}

	out := getConversationResponse{
		Conversation: mapConversation(conv),
		Turns:        mapTurns(turns),
	}
	writeJSON(w, http.StatusOK, out)
}

func mapConversation(c advisor.Conversation) conversationResponse {
	return conversationResponse{
		ID:        c.ID,
		TenantID:  string(c.TenantID),
		UserID:    c.UserID,
		Title:     c.Title,
		CreatedAt: c.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
		UpdatedAt: c.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
	}
}

func mapTurns(turns []advisor.Turn) []turnResponse {
	out := make([]turnResponse, 0, len(turns))
	for _, t := range turns {
		tr := turnResponse{
			ID:             t.ID,
			ConversationID: t.ConversationID,
			Role:           string(t.Role),
			Content:        t.Content,
			Sequence:       t.Sequence,
			LLMProvider:    t.LLMProvider,
			LLMModel:       t.LLMModel,
			InputTokens:    t.InputTokens,
			OutputTokens:   t.OutputTokens,
			CostUSD:        t.CostUSD,
			CreatedAt:      t.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
		}
		if len(t.ToolCalls) > 0 {
			tr.ToolCalls = make([]toolCallResponse, 0, len(t.ToolCalls))
			for _, tc := range t.ToolCalls {
				tr.ToolCalls = append(tr.ToolCalls, toolCallResponse{
					ID:         tc.ID,
					ToolName:   tc.ToolName,
					ArgsJSON:   string(tc.ArgsJSON),
					ResultJSON: string(tc.ResultJSON),
					Error:      tc.Error,
					DurationMs: tc.DurationMs,
				})
			}
		}
		out = append(out, tr)
	}
	return out
}

// advisorErrorStatus는 advisor 도메인 sentinel을 HTTP status로 매핑합니다.
//
//	ErrAdvisorDisabled       → 503 (옵트인 미활성)
//	ErrEmptyQuestion         → 400
//	ErrConversationNotFound  → 404
//	ErrUnknownTool           → 400
//	그 외                     → errorStatusFor fallback
func advisorErrorStatus(err error) int {
	switch {
	case errors.Is(err, advisor.ErrAdvisorDisabled):
		return http.StatusServiceUnavailable
	case errors.Is(err, advisor.ErrEmptyQuestion):
		return http.StatusBadRequest
	case errors.Is(err, advisor.ErrConversationNotFound):
		return http.StatusNotFound
	case errors.Is(err, advisor.ErrUnknownTool):
		return http.StatusBadRequest
	default:
		return errorStatusFor(err)
	}
}
