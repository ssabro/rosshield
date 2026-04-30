// llm_client.go — platform/llm.Adapter를 advisor.LLMClient로 wrapping.
//
// Phase 2 minimum: tool_use native 변환 미구현 — 어댑터의 Complete만 호출, plain text 반환.
// Anthropic native tool_use·Ollama function calling 통합은 후속 작업 (E16 추가 epic 또는 Phase 3).
//
// 주요 동작:
//   - LLM Adapter가 ErrLLMDisabled를 반환하면 그대로 propagate → orchestrator가 ErrAdvisorDisabled로 매핑
//   - tool 정의는 system prompt에 텍스트로 인라인 (LLM이 이해할 수 있도록)
//   - LLM 응답은 plain text — tool_use는 항상 빈 슬라이스
package advisorrun

import (
	"context"
	"fmt"
	"strings"

	"github.com/ssabro/rosshield/internal/domain/advisor"
	"github.com/ssabro/rosshield/internal/platform/llm"
)

// LLMClientAdapter는 platform/llm.Adapter를 advisor.LLMClient로 어댑팅합니다.
type LLMClientAdapter struct {
	adapter llm.Adapter
}

// NewLLMClient는 LLMClientAdapter를 반환합니다.
func NewLLMClient(adapter llm.Adapter) *LLMClientAdapter {
	return &LLMClientAdapter{adapter: adapter}
}

// CompleteWithTools는 messages를 LLM 어댑터로 전달하고 plain text 응답을 반환합니다.
//
// Phase 2 minimum: tool 정의는 system prompt에 인라인 텍스트로 포함되나,
// LLM 응답을 tool_use로 파싱하지 않음 — ToolCalls는 항상 비어있음.
// caller(orchestrator)가 ToolCalls 없으면 final assistant turn으로 처리.
func (c *LLMClientAdapter) CompleteWithTools(ctx context.Context, req advisor.LLMRequest) (advisor.LLMResponse, error) {
	if c.adapter == nil {
		return advisor.LLMResponse{}, fmt.Errorf("advisorrun: LLM adapter not configured")
	}

	// Messages 변환: advisor.LLMMessage → llm.Message.
	llmMessages := make([]llm.Message, 0, len(req.Messages)+1)

	// system prompt에 tool 정의를 텍스트로 인라인 (Phase 2 minimum — native tool_use 미구현).
	if len(req.Tools) > 0 {
		var sb strings.Builder
		sb.WriteString("Available read-only tools (Phase 2 minimum: invocation parsing not implemented; for now answer in natural language using the tool descriptions as background context):\n")
		for _, t := range req.Tools {
			fmt.Fprintf(&sb, "- %s: %s\n", t.Name, t.Description)
		}
		llmMessages = append(llmMessages, llm.Message{
			Role:    llm.RoleSystem,
			Content: sb.String(),
		})
	}

	for _, m := range req.Messages {
		switch m.Role {
		case advisor.RoleUser:
			llmMessages = append(llmMessages, llm.Message{Role: llm.RoleUser, Content: m.Content})
		case advisor.RoleAssistant:
			content := m.Content
			if content == "" && len(m.ToolCalls) > 0 {
				content = "(assistant invoked tools)"
			}
			llmMessages = append(llmMessages, llm.Message{Role: llm.RoleAssistant, Content: content})
		case advisor.RoleSystem:
			llmMessages = append(llmMessages, llm.Message{Role: llm.RoleSystem, Content: m.Content})
		case advisor.RoleTool:
			// tool 결과를 user 메시지로 fold (Anthropic 모델은 user role의 tool_result block을 기대하나, Phase 2는 단순화).
			llmMessages = append(llmMessages, llm.Message{
				Role:    llm.RoleUser,
				Content: fmt.Sprintf("Tool result for call %s:\n%s", m.ToolCallID, m.ToolResult),
			})
		}
	}

	llmReq := llm.CompleteRequest{
		Model:       req.Model,
		Messages:    llmMessages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}

	resp, err := c.adapter.Complete(ctx, llmReq)
	if err != nil {
		return advisor.LLMResponse{
			LLMProvider: resp.Trace.Provider,
			LLMModel:    resp.Trace.Model,
		}, err
	}

	return advisor.LLMResponse{
		Content:      resp.Content,
		StopReason:   resp.StopReason,
		LLMProvider:  resp.Trace.Provider,
		LLMModel:     resp.Trace.Model,
		InputTokens:  resp.Trace.InputTokens,
		OutputTokens: resp.Trace.OutputTokens,
		CostUSD:      resp.Trace.Cost,
		// ToolCalls는 항상 빈 슬라이스 — Phase 2 minimum.
	}, nil
}
