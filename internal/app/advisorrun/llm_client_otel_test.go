package advisorrun_test

// llm_client_otel_test.go — Phase 11.A-6 LLMClientAdapter span instrument 단위 검증.
//
// 전략:
//   - in-memory SpanRecorder 가 부착된 sdktrace.TracerProvider 로 tracer 주입.
//   - fake llm.Adapter (4 provider 시뮬레이션 — noop/anthropic/ollama/vllm) 가 LlmTrace
//     를 그대로 돌려주는 형태로 span attribute 매핑 검증.
//   - span name `llm.complete` + attribute (provider · model · tokens · cost · duration
//     · stop reason · tool count · disabled) 검증.
//   - error case: span.RecordError + Status(Error) 검증.
//   - noop ErrLLMDisabled: span 은 emit 되되 status Error 아님, disabled attribute true.

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/ssabro/rosshield/internal/app/advisorrun"
	"github.com/ssabro/rosshield/internal/domain/advisor"
	"github.com/ssabro/rosshield/internal/platform/llm"
	platformotel "github.com/ssabro/rosshield/internal/platform/otel"
)

// === fake llm.Adapter ===

type fakeLLMAdapter struct {
	provider string
	resp     llm.CompleteResponse
	err      error
}

func (f *fakeLLMAdapter) Provider() string { return f.provider }

func (f *fakeLLMAdapter) Complete(_ context.Context, _ llm.CompleteRequest) (llm.CompleteResponse, error) {
	return f.resp, f.err
}

func (f *fakeLLMAdapter) CompleteStream(_ context.Context, _ llm.CompleteRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 1)
	close(ch)
	return ch, nil
}

// === helpers ===

func newRecordingTracer(t *testing.T) (*tracetest.SpanRecorder, trace.Tracer) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(rec),
	)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	})
	return rec, tp.Tracer("rosshield/advisorrun-test")
}

func findAttr(stub tracetest.SpanStub, key string) (attribute.Value, bool) {
	for _, kv := range stub.Attributes {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// firstSpanStub 은 recorder 의 첫 번째 ended span 을 SpanStub 으로 변환합니다.
//
// rec.Ended() 가 []sdktrace.ReadOnlySpan 이므로 attribute query 가 가능한
// SpanStub 으로 한 번 변환해야 합니다.
func firstSpanStub(t *testing.T, rec *tracetest.SpanRecorder) tracetest.SpanStub {
	t.Helper()
	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	return tracetest.SpanStubFromReadOnlySpan(spans[0])
}

// === tests ===

// TestLLMClient_Anthropic_Span_Attributes 는 anthropic 성공 호출 시 span attribute
// 가 design §6.6 명시대로 부착되는지 검증합니다.
func TestLLMClient_Anthropic_Span_Attributes(t *testing.T) {
	t.Parallel()

	rec, tracer := newRecordingTracer(t)
	adapter := &fakeLLMAdapter{
		provider: "anthropic",
		resp: llm.CompleteResponse{
			Content:      "ok",
			InputTokens:  12,
			OutputTokens: 5,
			StopReason:   "end_turn",
			Trace: llm.LlmTrace{
				Provider:     "anthropic",
				Model:        "claude-3-haiku-20240307",
				InputTokens:  12,
				OutputTokens: 5,
				Cost:         0.0015,
				DurationMs:   42,
			},
		},
	}
	client := advisorrun.NewLLMClientWithTracer(adapter, tracer)

	resp, err := client.CompleteWithTools(context.Background(), advisor.LLMRequest{
		Model:    "claude-3-haiku-20240307",
		Messages: []advisor.LLMMessage{{Role: advisor.RoleUser, Content: "hi"}},
		Tools:    []advisor.ToolDefinition{{Name: "get_check", Description: "x"}},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content=%q", resp.Content)
	}

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	stub := tracetest.SpanStubFromReadOnlySpan(spans[0])
	if stub.Name != platformotel.LLMSpanComplete {
		t.Fatalf("span name=%q, want %q", stub.Name, platformotel.LLMSpanComplete)
	}
	if stub.SpanKind != trace.SpanKindClient {
		t.Fatalf("span kind=%v, want client", stub.SpanKind)
	}

	tests := map[string]attribute.Value{
		platformotel.AttrLLMProvider:     attribute.StringValue("anthropic"),
		platformotel.AttrLLMModel:        attribute.StringValue("claude-3-haiku-20240307"),
		platformotel.AttrLLMInputTokens:  attribute.IntValue(12),
		platformotel.AttrLLMOutputTokens: attribute.IntValue(5),
		platformotel.AttrLLMCostUSD:      attribute.Float64Value(0.0015),
		platformotel.AttrLLMStopReason:   attribute.StringValue("end_turn"),
		platformotel.AttrLLMToolCount:    attribute.IntValue(1),
	}
	for key, want := range tests {
		got, ok := findAttr(stub, key)
		if !ok {
			t.Errorf("attribute %q missing", key)
			continue
		}
		if got.Emit() != want.Emit() {
			t.Errorf("attribute %q = %v, want %v", key, got.Emit(), want.Emit())
		}
	}

	// duration attribute 는 wall-clock — 0 이상이기만 하면 됨.
	dur, ok := findAttr(stub, platformotel.AttrLLMDurationMs)
	if !ok {
		t.Errorf("duration attribute missing")
	} else if dur.AsInt64() < 0 {
		t.Errorf("duration negative: %d", dur.AsInt64())
	}

	// PII 회피: prompt/response content · tool name attribute 가 없어야 함.
	for _, kv := range stub.Attributes {
		key := string(kv.Key)
		if key == "llm.prompt" || key == "llm.response" || key == "llm.tool_names" ||
			key == "llm.tool_args" || key == "llm.content" {
			t.Errorf("PII attribute leaked: %q", key)
		}
	}
}

// TestLLMClient_Ollama_Span 은 ollama 어댑터의 LlmTrace 매핑을 검증합니다.
func TestLLMClient_Ollama_Span(t *testing.T) {
	t.Parallel()
	rec, tracer := newRecordingTracer(t)
	adapter := &fakeLLMAdapter{
		provider: "ollama",
		resp: llm.CompleteResponse{
			Content:      "hi",
			InputTokens:  3,
			OutputTokens: 7,
			StopReason:   "end_turn",
			Trace: llm.LlmTrace{
				Provider:     "ollama",
				Model:        "llama3.2",
				InputTokens:  3,
				OutputTokens: 7,
				Cost:         0,
				DurationMs:   100,
			},
		},
	}
	client := advisorrun.NewLLMClientWithTracer(adapter, tracer)
	if _, err := client.CompleteWithTools(context.Background(), advisor.LLMRequest{
		Model:    "llama3.2",
		Messages: []advisor.LLMMessage{{Role: advisor.RoleUser, Content: "q"}},
	}); err != nil {
		t.Fatalf("err=%v", err)
	}
	stub := firstSpanStub(t, rec)
	if v, _ := findAttr(stub, platformotel.AttrLLMProvider); v.AsString() != "ollama" {
		t.Errorf("provider=%q", v.AsString())
	}
	if v, _ := findAttr(stub, platformotel.AttrLLMModel); v.AsString() != "llama3.2" {
		t.Errorf("model=%q", v.AsString())
	}
	if v, _ := findAttr(stub, platformotel.AttrLLMToolCount); v.AsInt64() != 0 {
		t.Errorf("tool_count=%d, want 0", v.AsInt64())
	}
}

// TestLLMClient_VLLM_Span 은 vllm 어댑터의 LlmTrace 매핑을 검증합니다.
func TestLLMClient_VLLM_Span(t *testing.T) {
	t.Parallel()
	rec, tracer := newRecordingTracer(t)
	adapter := &fakeLLMAdapter{
		provider: "vllm",
		resp: llm.CompleteResponse{
			Content:      "v",
			InputTokens:  10,
			OutputTokens: 20,
			StopReason:   "end_turn",
			Trace: llm.LlmTrace{
				Provider:     "vllm",
				Model:        "meta-llama/Llama-3.1-8B-Instruct",
				InputTokens:  10,
				OutputTokens: 20,
				DurationMs:   200,
			},
		},
	}
	client := advisorrun.NewLLMClientWithTracer(adapter, tracer)
	if _, err := client.CompleteWithTools(context.Background(), advisor.LLMRequest{
		Model:    "meta-llama/Llama-3.1-8B-Instruct",
		Messages: []advisor.LLMMessage{{Role: advisor.RoleUser, Content: "q"}},
	}); err != nil {
		t.Fatalf("err=%v", err)
	}
	stub := firstSpanStub(t, rec)
	if v, _ := findAttr(stub, platformotel.AttrLLMProvider); v.AsString() != "vllm" {
		t.Errorf("provider=%q", v.AsString())
	}
	if v, _ := findAttr(stub, platformotel.AttrLLMInputTokens); v.AsInt64() != 10 {
		t.Errorf("input_tokens=%d", v.AsInt64())
	}
	if v, _ := findAttr(stub, platformotel.AttrLLMOutputTokens); v.AsInt64() != 20 {
		t.Errorf("output_tokens=%d", v.AsInt64())
	}
}

// TestLLMClient_Noop_DisabledOutcome 는 noop provider 의 ErrLLMDisabled 가 span
// status Error 가 되지 않고 disabled attribute 만 부착됨을 검증합니다.
func TestLLMClient_Noop_DisabledOutcome(t *testing.T) {
	t.Parallel()
	rec, tracer := newRecordingTracer(t)
	adapter := &fakeLLMAdapter{
		provider: "noop",
		resp: llm.CompleteResponse{
			Trace: llm.LlmTrace{Provider: "noop", Error: llm.ErrLLMDisabled.Error()},
		},
		err: llm.ErrLLMDisabled,
	}
	client := advisorrun.NewLLMClientWithTracer(adapter, tracer)
	_, err := client.CompleteWithTools(context.Background(), advisor.LLMRequest{
		Messages: []advisor.LLMMessage{{Role: advisor.RoleUser, Content: "q"}},
	})
	if !errors.Is(err, llm.ErrLLMDisabled) {
		t.Fatalf("err=%v, want ErrLLMDisabled", err)
	}
	stub := firstSpanStub(t, rec)
	if stub.Status.Code == codes.Error {
		t.Errorf("disabled outcome should not be Error status, got %v", stub.Status)
	}
	v, ok := findAttr(stub, platformotel.AttrLLMDisabled)
	if !ok || !v.AsBool() {
		t.Errorf("disabled attribute missing or false: ok=%v val=%v", ok, v.AsBool())
	}
	if len(stub.Events) != 0 {
		t.Errorf("disabled should not emit error event, got %d events", len(stub.Events))
	}
}

// TestLLMClient_Error_SetsSpanStatus 는 non-disabled error 가 span.RecordError +
// Status(Error) 를 emit 함을 검증합니다.
func TestLLMClient_Error_SetsSpanStatus(t *testing.T) {
	t.Parallel()
	rec, tracer := newRecordingTracer(t)
	testErr := errors.New("anthropic: http 500")
	adapter := &fakeLLMAdapter{
		provider: "anthropic",
		resp: llm.CompleteResponse{
			Trace: llm.LlmTrace{Provider: "anthropic", Model: "claude-3-haiku-20240307", Error: testErr.Error()},
		},
		err: testErr,
	}
	client := advisorrun.NewLLMClientWithTracer(adapter, tracer)
	_, err := client.CompleteWithTools(context.Background(), advisor.LLMRequest{
		Model:    "claude-3-haiku-20240307",
		Messages: []advisor.LLMMessage{{Role: advisor.RoleUser, Content: "q"}},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	stub := firstSpanStub(t, rec)
	if stub.Status.Code != codes.Error {
		t.Errorf("status=%v, want Error", stub.Status.Code)
	}
	if len(stub.Events) == 0 {
		t.Errorf("expected RecordError event, got 0")
	}
	// disabled attribute 는 false (ErrLLMDisabled 가 아닌 경우 부착 0).
	if v, ok := findAttr(stub, platformotel.AttrLLMDisabled); ok && v.AsBool() {
		t.Errorf("disabled true on non-disabled error")
	}
}

// TestLLMClient_NilTracer_FallsBackToNoop 는 NewLLMClient (tracer 미주입) 가
// noop tracer 로 fallback 되어 span emit 0 이지만 panic 없이 동작함을 검증합니다.
func TestLLMClient_NilTracer_FallsBackToNoop(t *testing.T) {
	t.Parallel()
	adapter := &fakeLLMAdapter{
		provider: "anthropic",
		resp: llm.CompleteResponse{
			Content: "ok",
			Trace:   llm.LlmTrace{Provider: "anthropic", Model: "m"},
		},
	}
	client := advisorrun.NewLLMClient(adapter) // tracer 미주입 → noop fallback
	resp, err := client.CompleteWithTools(context.Background(), advisor.LLMRequest{
		Messages: []advisor.LLMMessage{{Role: advisor.RoleUser, Content: "q"}},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content=%q", resp.Content)
	}
}

// TestLLMClient_NewLLMClientWithTracer_NilTracer 는 NewLLMClientWithTracer 에 nil
// tracer 주입 시 panic 없이 noop tracer fallback 함을 검증합니다.
func TestLLMClient_NewLLMClientWithTracer_NilTracer(t *testing.T) {
	t.Parallel()
	adapter := &fakeLLMAdapter{
		provider: "noop",
		resp:     llm.CompleteResponse{Trace: llm.LlmTrace{Provider: "noop"}},
		err:      llm.ErrLLMDisabled,
	}
	client := advisorrun.NewLLMClientWithTracer(adapter, nil)
	_, err := client.CompleteWithTools(context.Background(), advisor.LLMRequest{
		Messages: []advisor.LLMMessage{{Role: advisor.RoleUser, Content: "q"}},
	})
	if !errors.Is(err, llm.ErrLLMDisabled) {
		t.Fatalf("err=%v, want ErrLLMDisabled", err)
	}
}
