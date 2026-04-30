// Package advisor는 LLM 기반 자연어 대화 오케스트레이터입니다 (E16 Phase 2, 옵트인).
//
// 책임:
//
//   - 사용자가 "이 check가 왜 fail인가?" 같은 자연어 질문 수용
//   - LLM에 대화 컨텍스트 + read-only tool 정의 전달
//   - LLM이 호출한 tool dispatch (write API 절대 금지)
//   - tool 결과 redaction(E7) 후 LLM에 전달
//   - 최종 답변 생성 → Conversation/Turn/ToolCall 영속 + audit emit
//
// 도메인 결합 규칙 (P5 + P2 옵트인):
//
//	advisor 도메인은 platform/llm·domain/* (read-only Service interface)을 호출하나,
//	직접 import는 ToolDispatcher interface 통해서만. cmd/* bootstrap이 결선.
//	LLM Adapter가 noop이면 모든 호출이 ErrAdvisorDisabled 반환 (P2 옵트인 fallback).
//
// 결정: phase2-backlog.md E16 (1주 추정).
package advisor

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// Role은 Turn의 message role입니다 (LLM API 호환).
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool" // tool 호출 결과
)

// Conversation은 한 사용자의 대화 세션입니다.
type Conversation struct {
	ID        string
	TenantID  storage.TenantID
	UserID    string
	Title     string // 첫 user 메시지 첫 80자 자동
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Turn은 conversation 안 message 1건입니다.
//
// LLM 호출 시 LlmTrace 메타(provider/model/tokens/cost)를 갱신해 audit/cost 추적.
// user/system/tool turn은 cost 0.
type Turn struct {
	ID             string
	ConversationID string
	TenantID       storage.TenantID
	Role           Role
	Content        string // assistant 응답 본문 또는 user input
	Sequence       int    // conversation 안 순서 (0부터)
	LLMProvider    string
	LLMModel       string
	InputTokens    int
	OutputTokens   int
	CostUSD        float64
	CreatedAt      time.Time

	// ToolCalls는 본 assistant turn이 호출한 tool 목록 (DB는 별도 테이블).
	ToolCalls []ToolCall
}

// ToolCall은 LLM이 한 read-only tool 호출 1건입니다.
//
// ResultJSON은 redaction(E7) 거친 후 영속 — 자격 증명·secret 절대 평문 금지.
type ToolCall struct {
	ID         string
	TurnID     string
	TenantID   storage.TenantID
	ToolName   string
	ArgsJSON   json.RawMessage
	ResultJSON json.RawMessage // redacted
	Error      string          // 빈 값이면 성공
	DurationMs int64
	CreatedAt  time.Time
}

// AskRequest는 Service.Ask 입력입니다 (single-shot 흐름).
//
// ConversationID가 빈 값이면 새 conversation 생성. 비어있지 않으면 기존 conversation에 turn 추가.
// MaxToolCalls는 LLM이 호출 가능한 tool call 상한 (무한 loop 방지). 0이면 default 5.
type AskRequest struct {
	ConversationID string // 옵션 — 비어있으면 신규
	UserID         string
	Question       string
	MaxToolCalls   int
}

// AskResponse는 Service.Ask 출력입니다.
//
// ConversationID는 항상 채워짐(신규 또는 재사용). FinalAnswer는 LLM의 마지막 assistant
// turn 본문. Turns는 본 호출에서 추가된 모든 turn (user 1 + assistant N + tool M).
type AskResponse struct {
	ConversationID string
	FinalAnswer    string
	Turns          []Turn
}

// AuditEmitter는 advisor 도메인 변경을 audit chain에 기록하는 콜백입니다 (P5).
//
// 호출 시점:
//
//	StartConversation       → EmitConversationStarted
//	각 ToolCall (성공/실패)   → EmitToolCalled
//	최종 assistant turn      → EmitAdvisorResponded
type AuditEmitter interface {
	EmitConversationStarted(ctx context.Context, tx storage.Tx, c Conversation) error
	EmitToolCalled(ctx context.Context, tx storage.Tx, c ToolCall) error
	EmitAdvisorResponded(ctx context.Context, tx storage.Tx, t Turn) error
}

// LLMClient는 advisor가 필요한 LLM 어댑터의 minimal 표면입니다 (P5 — platform/llm 직접 import 회피).
//
// CompleteWithTools는 messages + tool 정의를 받아 응답을 반환:
//   - LLM이 tool을 호출하면 ToolCalls 채움 (각 ToolCallRequest는 name + args)
//   - 응답 본문이 있으면 Content 채움
//   - LlmTrace는 LlmProvider/Model/tokens/cost 메타 (audit cross-check용)
//
// noop 어댑터는 ErrLLMDisabled 반환.
type LLMClient interface {
	CompleteWithTools(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

// LLMRequest는 LLMClient.CompleteWithTools 입력입니다.
type LLMRequest struct {
	Model       string
	Messages    []LLMMessage
	Tools       []ToolDefinition
	Temperature float64
	MaxTokens   int
}

// LLMMessage는 LLM 호출 시 컨텍스트 message 1건입니다.
type LLMMessage struct {
	Role       Role
	Content    string            // 텍스트 본문
	ToolCalls  []ToolCallRequest // assistant role + LLM이 호출 결정한 tool들
	ToolCallID string            // tool role + assistant.ToolCalls 중 어떤 호출에 대한 응답인지
	ToolResult string            // tool role + 결과 본문
}

// LLMResponse는 LLMClient.CompleteWithTools 출력입니다.
type LLMResponse struct {
	Content      string            // assistant 응답 본문 (tool call만 있고 본문 없으면 빈 값)
	ToolCalls    []ToolCallRequest // LLM이 호출 결정한 tool들
	StopReason   string            // "end_turn"|"tool_use"|"max_tokens"
	LLMProvider  string
	LLMModel     string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
}

// ToolCallRequest는 LLM이 호출 결정한 tool 1건입니다.
type ToolCallRequest struct {
	ID       string          // LLM이 부여한 호출 ID
	ToolName string          // "get_check" 등
	ArgsJSON json.RawMessage // JSON 직렬화 입력
}

// ToolDefinition은 LLM에 노출할 tool 메타입니다 (Anthropic Messages API tool_use 호환).
//
// Phase 2는 read-only tool 7종 — write 시도는 dispatcher가 거부.
type ToolDefinition struct {
	Name        string          // "get_check"
	Description string          // LLM이 어떤 상황에 호출할지 판단하는 안내
	Schema      json.RawMessage // JSON Schema (Anthropic input_schema 형식)
}

// ToolDispatcher는 LLM이 호출 결정한 tool을 실행합니다 (P5 — advisor가 다른 도메인 직접 호출 X).
//
// bootstrap이 7종 read-only tool을 등록한 dispatcher를 주입.
type ToolDispatcher interface {
	// AvailableTools는 LLM 컨텍스트에 포함할 tool 정의 목록을 반환합니다.
	AvailableTools() []ToolDefinition

	// Dispatch는 단일 tool 호출을 실행합니다. tenant scope tx로 진입.
	// 알 수 없는 tool은 ErrUnknownTool. write 시도 (등록 안 된 tool명)는 자동 거부.
	// 결과는 redaction(E7) 거친 JSON.
	Dispatch(ctx context.Context, tx storage.Tx, req ToolCallRequest) (ToolCallResult, error)
}

// ToolCallResult는 ToolDispatcher.Dispatch 출력입니다.
type ToolCallResult struct {
	ResultJSON json.RawMessage // redacted JSON
	DurationMs int64
}

// Service는 advisor 도메인 진입점입니다 (E16).
type Service interface {
	// Ask는 user 질문을 받아 LLM 호출 + tool dispatch loop를 실행하고 최종 답변을 반환합니다.
	//
	// LLM이 ErrLLMDisabled를 반환하면 ErrAdvisorDisabled (P2 옵트인 fallback).
	// MaxToolCalls 초과 시 tool loop 종료 + 부분 답변 반환 (audit emit).
	Ask(ctx context.Context, tx storage.Tx, req AskRequest) (AskResponse, error)

	// GetConversation은 conversation + 모든 turn(seq ASC)을 반환합니다.
	GetConversation(ctx context.Context, tx storage.Tx, conversationID string) (Conversation, []Turn, error)

	// ListConversations는 tenant·user 단위 대화를 updated_at DESC로 반환합니다.
	ListConversations(ctx context.Context, tx storage.Tx, userID string, limit int) ([]Conversation, error)
}

// 공통 sentinel.
var (
	ErrConversationNotFound = errors.New("advisor: conversation not found")
	ErrAdvisorDisabled      = errors.New("advisor: LLM provider disabled (use ollama/anthropic to enable)")
	ErrUnknownTool          = errors.New("advisor: unknown tool")
	ErrToolDispatchFailed   = errors.New("advisor: tool dispatch failed")
	ErrEmptyQuestion        = errors.New("advisor: question is required")
)

// DefaultMaxToolCalls는 단일 Ask 흐름의 tool 호출 상한입니다 (R14-6 cost guardrail).
const DefaultMaxToolCalls = 5

// DefaultTitleLength는 conversation title 자동 생성 시 첫 user 메시지에서 자르는 글자 수입니다.
const DefaultTitleLength = 80

// MakeTitle은 user 첫 message에서 title을 추출합니다 (rune-safe).
func MakeTitle(question string) string {
	r := []rune(question)
	if len(r) <= DefaultTitleLength {
		return string(r)
	}
	return string(r[:DefaultTitleLength])
}
