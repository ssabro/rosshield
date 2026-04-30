// Package noop는 LLM 기능을 비활성화하는 기본 어댑터입니다 (R14-1).
//
// 모든 호출이 ErrLLMDisabled로 즉시 실패하므로, caller(Insight·Advisor 도메인)는
// 결정론적 fallback 경로(P6)를 사용해야 합니다. Phase 2 기본값 — config에서
// "ollama" 또는 "anthropic"으로 오버라이드해야 LLM 기능이 동작합니다.
package noop

import (
	"context"
	"time"

	"github.com/ssabro/rosshield/internal/platform/llm"
)

// Adapter는 ErrLLMDisabled를 즉시 반환하는 stub 어댑터입니다.
type Adapter struct{}

// New는 새 noop Adapter를 반환합니다.
func New() *Adapter { return &Adapter{} }

// Provider는 식별자 "noop"을 반환합니다.
func (*Adapter) Provider() string { return "noop" }

// Complete은 ErrLLMDisabled를 반환합니다 — content/token은 전부 비어있고,
// Trace.Error는 sentinel 메시지를 담습니다 (audit emit에 그대로 사용).
func (*Adapter) Complete(_ context.Context, req llm.CompleteRequest) (llm.CompleteResponse, error) {
	now := time.Now().UTC()
	resp := llm.CompleteResponse{
		Trace: llm.LlmTrace{
			Provider:   "noop",
			Model:      req.Model,
			StartedAt:  now,
			DurationMs: 0,
			Error:      llm.ErrLLMDisabled.Error(),
		},
	}
	return resp, llm.ErrLLMDisabled
}

// CompleteStream은 단 한 개의 종료 chunk(Done=true, Err=ErrLLMDisabled)만 보낸 뒤
// 채널을 닫습니다 — caller는 일반 stream 종료 흐름과 동일하게 처리할 수 있습니다.
func (a *Adapter) CompleteStream(_ context.Context, req llm.CompleteRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 1)
	now := time.Now().UTC()
	ch <- llm.StreamChunk{
		Done: true,
		Err:  llm.ErrLLMDisabled,
		Trace: llm.LlmTrace{
			Provider:   "noop",
			Model:      req.Model,
			StartedAt:  now,
			DurationMs: 0,
			Error:      llm.ErrLLMDisabled.Error(),
		},
	}
	close(ch)
	return ch, nil
}
