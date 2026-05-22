package otel_test

// llm_test.go — Phase 11.A-6 LlmTrace ↔ span attribute 매핑 helper 단위 검증.

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"

	platformotel "github.com/ssabro/rosshield/internal/platform/otel"
)

func TestLLMInputAttributes_FullSet(t *testing.T) {
	t.Parallel()
	attrs := platformotel.LLMInputAttributes(platformotel.LLMSpanInput{
		Provider:  "anthropic",
		Model:     "claude-3-haiku-20240307",
		ToolCount: 3,
	})
	got := indexByKey(attrs)
	if v, ok := got[platformotel.AttrLLMProvider]; !ok || v.AsString() != "anthropic" {
		t.Errorf("provider: ok=%v val=%v", ok, v.AsString())
	}
	if v, ok := got[platformotel.AttrLLMModel]; !ok || v.AsString() != "claude-3-haiku-20240307" {
		t.Errorf("model: ok=%v val=%v", ok, v.AsString())
	}
	if v, ok := got[platformotel.AttrLLMToolCount]; !ok || v.AsInt64() != 3 {
		t.Errorf("tool_count: ok=%v val=%v", ok, v.AsInt64())
	}
}

// TestLLMInputAttributes_EmptyProviderModelOmitted 는 provider/model 이 빈 문자열이면
// 해당 attribute 가 생략되는지 검증 (잡음 회피).
func TestLLMInputAttributes_EmptyProviderModelOmitted(t *testing.T) {
	t.Parallel()
	attrs := platformotel.LLMInputAttributes(platformotel.LLMSpanInput{ToolCount: 0})
	got := indexByKey(attrs)
	if _, ok := got[platformotel.AttrLLMProvider]; ok {
		t.Errorf("provider attribute should be omitted")
	}
	if _, ok := got[platformotel.AttrLLMModel]; ok {
		t.Errorf("model attribute should be omitted")
	}
	if v, ok := got[platformotel.AttrLLMToolCount]; !ok || v.AsInt64() != 0 {
		t.Errorf("tool_count should always be attached: ok=%v val=%v", ok, v.AsInt64())
	}
}

func TestLLMOutcomeAttributes_FullSet(t *testing.T) {
	t.Parallel()
	attrs := platformotel.LLMOutcomeAttributes(platformotel.LLMSpanOutcome{
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.0125,
		DurationMs:   1234,
		StopReason:   "end_turn",
	})
	got := indexByKey(attrs)
	if v, ok := got[platformotel.AttrLLMInputTokens]; !ok || v.AsInt64() != 100 {
		t.Errorf("input_tokens=%v ok=%v", v.AsInt64(), ok)
	}
	if v, ok := got[platformotel.AttrLLMOutputTokens]; !ok || v.AsInt64() != 50 {
		t.Errorf("output_tokens=%v ok=%v", v.AsInt64(), ok)
	}
	if v, ok := got[platformotel.AttrLLMCostUSD]; !ok || v.AsFloat64() != 0.0125 {
		t.Errorf("cost=%v ok=%v", v.AsFloat64(), ok)
	}
	if v, ok := got[platformotel.AttrLLMDurationMs]; !ok || v.AsInt64() != 1234 {
		t.Errorf("duration=%v ok=%v", v.AsInt64(), ok)
	}
	if v, ok := got[platformotel.AttrLLMStopReason]; !ok || v.AsString() != "end_turn" {
		t.Errorf("stop_reason=%v ok=%v", v.AsString(), ok)
	}
	if _, ok := got[platformotel.AttrLLMDisabled]; ok {
		t.Errorf("disabled should be omitted when false")
	}
}

func TestLLMOutcomeAttributes_DisabledAttached(t *testing.T) {
	t.Parallel()
	attrs := platformotel.LLMOutcomeAttributes(platformotel.LLMSpanOutcome{Disabled: true})
	got := indexByKey(attrs)
	if v, ok := got[platformotel.AttrLLMDisabled]; !ok || !v.AsBool() {
		t.Errorf("disabled should be true: ok=%v val=%v", ok, v.AsBool())
	}
}

func indexByKey(attrs []attribute.KeyValue) map[string]attribute.Value {
	out := make(map[string]attribute.Value, len(attrs))
	for _, kv := range attrs {
		out[string(kv.Key)] = kv.Value
	}
	return out
}
