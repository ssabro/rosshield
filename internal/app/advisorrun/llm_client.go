// llm_client.go — platform/llm.Adapter를 advisor.LLMClient로 wrapping.
//
// Phase 2 minimum: tool_use native 변환 미구현 — 어댑터의 Complete만 호출, plain text 반환.
// Anthropic native tool_use·Ollama function calling 통합은 후속 작업 (E16 추가 epic 또는 Phase 3).
//
// 주요 동작:
//   - LLM Adapter가 ErrLLMDisabled를 반환하면 그대로 propagate → orchestrator가 ErrAdvisorDisabled로 매핑
//   - tool 정의는 system prompt에 텍스트로 인라인 (LLM이 이해할 수 있도록)
//   - LLM 응답은 plain text — tool_use는 항상 빈 슬라이스
//
// Phase 11.A-6 OpenTelemetry trace 결선:
//   - tracer 가 nil 이면 noop tracer fallback — overhead 0 (R14-1 옵트인 일관).
//   - 모든 CompleteWithTools 호출은 `llm.complete` span 1 개 emit.
//   - attribute: llm.provider · llm.model · llm.tokens.input/output · llm.cost.usd
//     · llm.duration.ms · llm.stop_reason · llm.tool_count · llm.error (PII 회피 엄격).
//   - prompt/response content · tool name · tool args 는 attribute 미부착 (D-LLM-3).
package advisorrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/ssabro/rosshield/internal/domain/advisor"
	"github.com/ssabro/rosshield/internal/platform/llm"
	platformotel "github.com/ssabro/rosshield/internal/platform/otel"
)

// LLMClientAdapter는 platform/llm.Adapter를 advisor.LLMClient로 어댑팅합니다.
//
// tracer 필드는 Phase 11.A-6 — `llm.complete` span emit. nil 이면 noop tracer fallback.
type LLMClientAdapter struct {
	adapter llm.Adapter
	tracer  trace.Tracer
}

// NewLLMClient는 LLMClientAdapter를 반환합니다.
//
// 기존 호출자 호환: tracer 미주입 — fallback noop, span emit 0. WithTracer 로 추가 결선.
func NewLLMClient(adapter llm.Adapter) *LLMClientAdapter {
	return &LLMClientAdapter{
		adapter: adapter,
		tracer:  tracenoop.NewTracerProvider().Tracer(platformotel.LLMTracerScope),
	}
}

// NewLLMClientWithTracer 는 명시 tracer 와 함께 LLMClientAdapter 를 반환합니다.
//
// bootstrap 이 otelProvider.Tracer(LLMTracerScope) 로 주입 — Enabled=false 면 noop.
// tracer 가 nil 이면 noop tracer 로 fallback (panic 회피).
func NewLLMClientWithTracer(adapter llm.Adapter, tracer trace.Tracer) *LLMClientAdapter {
	if tracer == nil {
		tracer = tracenoop.NewTracerProvider().Tracer(platformotel.LLMTracerScope)
	}
	return &LLMClientAdapter{
		adapter: adapter,
		tracer:  tracer,
	}
}

// CompleteWithTools는 messages를 LLM 어댑터로 전달하고 plain text 응답을 반환합니다.
//
// Phase 2 minimum: tool 정의는 system prompt에 인라인 텍스트로 포함되나,
// LLM 응답을 tool_use로 파싱하지 않음 — ToolCalls는 항상 비어있음.
// caller(orchestrator)가 ToolCalls 없으면 final assistant turn으로 처리.
//
// Phase 11.A-6: `llm.complete` span 1 개 emit. PII 회피 엄격 (prompt/response 미부착).
func (c *LLMClientAdapter) CompleteWithTools(ctx context.Context, req advisor.LLMRequest) (advisor.LLMResponse, error) {
	if c.adapter == nil {
		return advisor.LLMResponse{}, fmt.Errorf("advisorrun: LLM adapter not configured")
	}

	provider := c.adapter.Provider()
	model := req.Model
	ctx, span := c.tracer.Start(ctx, platformotel.LLMSpanComplete,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(platformotel.LLMInputAttributes(platformotel.LLMSpanInput{
			Provider:  provider,
			Model:     model,
			ToolCount: len(req.Tools),
		})...),
	)
	defer span.End()

	started := time.Now()
	resp, err := c.invokeAdapter(ctx, req)
	c.finalizeSpan(span, resp, err, time.Since(started).Milliseconds())
	return resp, err
}

// invokeAdapter 는 LLM adapter 의 Complete 를 호출하고 advisor.LLMResponse 로 매핑합니다.
//
// span 결선과 분리 — finalizeSpan 이 attribute/error 부착 책임.
func (c *LLMClientAdapter) invokeAdapter(ctx context.Context, req advisor.LLMRequest) (advisor.LLMResponse, error) {
	llmMessages := buildLLMMessages(req)
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

// finalizeSpan 은 LLM 호출 outcome 을 span attribute 와 status 에 결선합니다.
//
// PII 회피 — token count(size only) · cost · duration · stop reason 만. prompt/response
// content 는 부착 0.
//
// Disabled outcome(noop provider 의 ErrLLMDisabled) 는 별도 attribute 로 표시하되
// span status 는 Error 로 설정하지 않음 — opt-in 비활성은 정상 운영 경로.
//
// durationMs 는 wall-clock 측정 (CompleteWithTools 진입 ~ adapter Complete 반환).
// llm.LlmTrace.DurationMs 가 있을 때는 그쪽이 더 정확(provider 내부 측정) — outcome
// attribute 는 wall-clock fallback.
func (c *LLMClientAdapter) finalizeSpan(span trace.Span, resp advisor.LLMResponse, callErr error, durationMs int64) {
	disabled := errors.Is(callErr, llm.ErrLLMDisabled)
	span.SetAttributes(platformotel.LLMOutcomeAttributes(platformotel.LLMSpanOutcome{
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		CostUSD:      resp.CostUSD,
		DurationMs:   durationMs,
		StopReason:   resp.StopReason,
		Disabled:     disabled,
	})...)
	if callErr == nil {
		return
	}
	if disabled {
		// noop fallback — error 로 표시하지 않음 (정상 opt-in 경로).
		return
	}
	span.RecordError(callErr)
	span.SetStatus(codes.Error, callErr.Error())
}

// buildLLMMessages 는 advisor.LLMRequest 를 llm.Message slice 로 변환합니다.
//
// system prompt 에 tool 정의를 텍스트로 인라인 (Phase 2 minimum — native tool_use 미구현).
func buildLLMMessages(req advisor.LLMRequest) []llm.Message {
	llmMessages := make([]llm.Message, 0, len(req.Messages)+1)

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
	return llmMessages
}
