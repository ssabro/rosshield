// orchestrator.go — E16-C advisor.Service 구현 (LLM + tool dispatch loop).
//
// 흐름 (Ask):
//
//  1. ConversationID 비어있으면 StartConversation
//  2. user turn AppendTurn (sequence 0)
//  3. LLM 호출 loop:
//     a. LLMClient.CompleteWithTools(messages, tools)
//     b. response.ToolCalls 있으면 각 tool Dispatch → tool turn AppendTurn → loop 계속
//     c. response.Content 있으면 final assistant turn AppendTurn → 종료
//     d. MaxToolCalls 초과 시 break + 부분 답변 저장
//
// MaxToolCalls는 R14-6 cost guardrail 보강 — 무한 loop 방지.
package advisorrun

import (
	"context"
	"fmt"
	"strings"

	"github.com/ssabro/rosshield/internal/domain/advisor"
	advisorrepo "github.com/ssabro/rosshield/internal/domain/advisor/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// OrchestratorDeps는 Orchestrator 생성 의존성입니다.
type OrchestratorDeps struct {
	Repo       *advisorrepo.Repo
	LLM        advisor.LLMClient
	Dispatcher advisor.ToolDispatcher
	// DefaultModel은 LLMRequest.Model이 비어있을 때 fallback. 빈 값이면 LLM 어댑터 기본값.
	DefaultModel string
}

// Orchestrator는 advisor.Service의 Ask 흐름을 구현합니다.
//
// 영속·tool dispatch는 Repo·Dispatcher 위임. LLM 호출은 LLMClient.
type Orchestrator struct {
	deps OrchestratorDeps
}

// NewOrchestrator는 새 Orchestrator를 반환합니다.
func NewOrchestrator(deps OrchestratorDeps) *Orchestrator {
	return &Orchestrator{deps: deps}
}

// Ask는 user 질문 → LLM/tool loop → 최종 답변 흐름을 실행합니다.
func (o *Orchestrator) Ask(ctx context.Context, tx storage.Tx, req advisor.AskRequest) (advisor.AskResponse, error) {
	if o.deps.LLM == nil {
		return advisor.AskResponse{}, advisor.ErrAdvisorDisabled
	}
	if o.deps.Dispatcher == nil {
		return advisor.AskResponse{}, fmt.Errorf("advisor: dispatcher not configured")
	}
	if strings.TrimSpace(req.Question) == "" {
		return advisor.AskResponse{}, advisor.ErrEmptyQuestion
	}
	maxCalls := req.MaxToolCalls
	if maxCalls <= 0 {
		maxCalls = advisor.DefaultMaxToolCalls
	}

	// 1) conversation 결정 — 신규 또는 재사용.
	conversationID := req.ConversationID
	var newTurns []advisor.Turn
	if conversationID == "" {
		conv, err := o.deps.Repo.StartConversation(ctx, tx, req.UserID, req.Question)
		if err != nil {
			return advisor.AskResponse{}, fmt.Errorf("start conversation: %w", err)
		}
		conversationID = conv.ID
	}

	// 2) user turn 추가.
	userTurn, err := o.deps.Repo.AppendTurn(ctx, tx, conversationID, advisor.Turn{
		Role:    advisor.RoleUser,
		Content: req.Question,
	})
	if err != nil {
		return advisor.AskResponse{}, fmt.Errorf("user turn: %w", err)
	}
	newTurns = append(newTurns, userTurn)

	// 3) LLM 호출 loop.
	messages := []advisor.LLMMessage{{Role: advisor.RoleUser, Content: req.Question}}
	tools := o.deps.Dispatcher.AvailableTools()

	finalContent := ""
	for callCount := 0; callCount <= maxCalls; callCount++ {
		llmReq := advisor.LLMRequest{
			Model:       o.deps.DefaultModel,
			Messages:    messages,
			Tools:       tools,
			Temperature: 0.0, // 결정성 우선
			MaxTokens:   2048,
		}
		resp, err := o.deps.LLM.CompleteWithTools(ctx, llmReq)
		if err != nil {
			// LLM ErrLLMDisabled 등은 ErrAdvisorDisabled로 매핑 (P2 옵트인 fallback).
			return advisor.AskResponse{
				ConversationID: conversationID,
				Turns:          newTurns,
			}, advisor.ErrAdvisorDisabled
		}

		// tool 호출이 있으면 dispatch + tool turn 추가, 계속 loop.
		if len(resp.ToolCalls) > 0 {
			if callCount >= maxCalls {
				// 한도 초과 — 마지막 응답을 final로 저장.
				finalContent = "(advisor: max tool calls reached, partial answer)"
				if resp.Content != "" {
					finalContent = resp.Content
				}
				break
			}
			// assistant turn (tool_use) 영속 — content는 비어있을 수 있음.
			toolCalls := make([]advisor.ToolCall, 0, len(resp.ToolCalls))
			messages = append(messages, advisor.LLMMessage{
				Role:      advisor.RoleAssistant,
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

			// 각 tool dispatch.
			for _, call := range resp.ToolCalls {
				result, derr := o.deps.Dispatcher.Dispatch(ctx, tx, call)
				tc := advisor.ToolCall{
					ToolName:   call.ToolName,
					ArgsJSON:   call.ArgsJSON,
					ResultJSON: result.ResultJSON,
					DurationMs: result.DurationMs,
				}
				if derr != nil {
					tc.Error = derr.Error()
				}
				toolCalls = append(toolCalls, tc)
				// tool 결과를 LLM 다음 호출 messages에 추가.
				messages = append(messages, advisor.LLMMessage{
					Role:       advisor.RoleTool,
					ToolCallID: call.ID,
					ToolResult: string(result.ResultJSON),
				})
			}
			// assistant turn (tool_use) 영속 — toolCalls 묶어서.
			astTurn, err := o.deps.Repo.AppendTurn(ctx, tx, conversationID, advisor.Turn{
				Role:         advisor.RoleAssistant,
				Content:      resp.Content, // 비어있을 수 있음 (tool_use only)
				LLMProvider:  resp.LLMProvider,
				LLMModel:     resp.LLMModel,
				InputTokens:  resp.InputTokens,
				OutputTokens: resp.OutputTokens,
				CostUSD:      resp.CostUSD,
				ToolCalls:    toolCalls,
			})
			if err != nil {
				return advisor.AskResponse{}, fmt.Errorf("assistant tool turn: %w", err)
			}
			newTurns = append(newTurns, astTurn)
			continue
		}

		// final assistant 답변 (content만, tool 호출 없음) — loop 종료.
		finalContent = resp.Content
		astTurn, err := o.deps.Repo.AppendTurn(ctx, tx, conversationID, advisor.Turn{
			Role:         advisor.RoleAssistant,
			Content:      resp.Content,
			LLMProvider:  resp.LLMProvider,
			LLMModel:     resp.LLMModel,
			InputTokens:  resp.InputTokens,
			OutputTokens: resp.OutputTokens,
			CostUSD:      resp.CostUSD,
		})
		if err != nil {
			return advisor.AskResponse{}, fmt.Errorf("final assistant turn: %w", err)
		}
		newTurns = append(newTurns, astTurn)
		break
	}

	return advisor.AskResponse{
		ConversationID: conversationID,
		FinalAnswer:    finalContent,
		Turns:          newTurns,
	}, nil
}

// GetConversation은 advisor.Service.GetConversation 구현 — Repo 위임.
func (o *Orchestrator) GetConversation(ctx context.Context, tx storage.Tx, conversationID string) (advisor.Conversation, []advisor.Turn, error) {
	return o.deps.Repo.GetConversation(ctx, tx, conversationID)
}

// ListConversations는 advisor.Service.ListConversations 구현 — Repo 위임.
func (o *Orchestrator) ListConversations(ctx context.Context, tx storage.Tx, userID string, limit int) ([]advisor.Conversation, error) {
	return o.deps.Repo.ListConversations(ctx, tx, userID, limit)
}
