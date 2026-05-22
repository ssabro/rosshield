// Package otel 의 llm.go — Phase 11.A-6 LLM advisor call trace helper.
//
// 책임:
//   - LlmTrace ↔ span attribute 매핑 단일 진실 source (design §6.6).
//   - 4 provider(noop · anthropic · ollama · vllm) 의 LlmTrace 결과를 동일 형식의
//     span attribute 로 변환 — caller(LLMClientAdapter) 가 1 호출로 결선.
//   - PII 회피 엄격 — prompt content · response content · tool args 는 attribute 에
//     포함하지 않음. token count(size only) · cost · duration · provider/model 식별자만.
//
// 도메인 경계:
//   - 본 helper 는 platform/otel — domain/advisor · application/advisorrun 어디서든
//     import 가능. llm 패키지 자체는 otel 의존 0 일관 (provider adapter 는 stdlib net/http 만).
//
// 결정 항목 추적:
//   - D-P11A-1 = 옵션 A (otel SDK 전면).
//   - design §6.6 Stage 11.A-6 — LLM 4 provider span instrument.
//   - D-LLM-3 = prompt/response 미기록 정책. attribute 에서도 동일 정책.

package otel

import (
	"go.opentelemetry.io/otel/attribute"
)

// LLM span 의 instrumentation scope · name 상수 — design §6.6 명시.
const (
	// LLMTracerScope 는 LLMClientAdapter 가 사용할 tracer scope 입니다.
	LLMTracerScope = "rosshield/advisorrun/llm"
	// LLMSpanComplete 는 LLMClientAdapter.CompleteWithTools 의 span name 입니다.
	LLMSpanComplete = "llm.complete"
)

// LLM span attribute key 상수 (design §6.6 명시).
//
// PII 회피 정책:
//   - prompt/response content 는 attribute 에 포함 0 (D-LLM-3).
//   - tool name · args 는 attribute 에 포함 0 (potential PII). tool_count(size only) 만.
//   - input/output tokens 는 size only — content 정보 노출 아님.
const (
	AttrLLMProvider     = "llm.provider"
	AttrLLMModel        = "llm.model"
	AttrLLMInputTokens  = "llm.tokens.input"
	AttrLLMOutputTokens = "llm.tokens.output"
	AttrLLMCostUSD      = "llm.cost.usd"
	AttrLLMDurationMs   = "llm.duration.ms"
	AttrLLMStopReason   = "llm.stop_reason"
	AttrLLMToolCount    = "llm.tool_count"
	AttrLLMError        = "llm.error"
	// AttrLLMDisabled 는 noop provider 경로 식별용 — outcome 분류에 사용.
	AttrLLMDisabled = "llm.disabled"
)

// LLMSpanInput 은 CompleteWithTools 호출 시점에 알 수 있는 입력 attribute 입니다.
//
// span 시작 시점에 한 번 부착 — provider/model 식별자는 LLM 호출 전에 결정.
type LLMSpanInput struct {
	Provider  string // "noop"|"anthropic"|"ollama"|"vllm"
	Model     string // 어댑터별 모델 식별자
	ToolCount int    // 호출에 전달된 tool 개수 (size only, PII 회피)
}

// LLMSpanOutcome 은 CompleteWithTools 응답 후 결정되는 outcome attribute 입니다.
//
// 응답의 LlmTrace 에서 채워지며, error 경로에서도 부분 채움 (provider/model 만 알 수도).
type LLMSpanOutcome struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	DurationMs   int64
	StopReason   string
	Disabled     bool // noop provider 의 ErrLLMDisabled outcome 식별
}

// LLMInputAttributes 는 LLMSpanInput 을 attribute.KeyValue slice 로 변환합니다.
//
// span 시작 시 trace.WithAttributes(...) 에 전달. provider/model 이 빈 문자열이면
// 해당 attribute 는 생략 — 부착 잡음 회피.
func LLMInputAttributes(in LLMSpanInput) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 3)
	if in.Provider != "" {
		attrs = append(attrs, attribute.String(AttrLLMProvider, in.Provider))
	}
	if in.Model != "" {
		attrs = append(attrs, attribute.String(AttrLLMModel, in.Model))
	}
	attrs = append(attrs, attribute.Int(AttrLLMToolCount, in.ToolCount))
	return attrs
}

// LLMOutcomeAttributes 는 LLMSpanOutcome 을 attribute.KeyValue slice 로 변환합니다.
//
// span 종료 직전 span.SetAttributes(...) 에 전달. token/cost/duration 은 항상 부착
// (0 도 의미가 있음). stop reason 은 빈 값이면 생략.
func LLMOutcomeAttributes(out LLMSpanOutcome) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 6)
	attrs = append(attrs,
		attribute.Int(AttrLLMInputTokens, out.InputTokens),
		attribute.Int(AttrLLMOutputTokens, out.OutputTokens),
		attribute.Float64(AttrLLMCostUSD, out.CostUSD),
		attribute.Int64(AttrLLMDurationMs, out.DurationMs),
	)
	if out.StopReason != "" {
		attrs = append(attrs, attribute.String(AttrLLMStopReason, out.StopReason))
	}
	if out.Disabled {
		attrs = append(attrs, attribute.Bool(AttrLLMDisabled, true))
	}
	return attrs
}
