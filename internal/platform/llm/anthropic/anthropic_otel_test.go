package anthropic_test

// anthropic_otel_test.go — Phase 11.A-6 outbound HTTP transport otel wrap 단위 검증.
//
// 전략:
//   - httptest.NewServer 가 incoming request 의 `traceparent` header 를 캡처.
//   - httpclient.WrapClient 로 wrap 된 http.Client 를 anthropic.New 의 Options.HTTPClient
//     로 주입 → outbound 요청에 W3C traceparent header 가 자동 inject 되는지 확인.
//   - tracer 가 active span 을 만든 ctx 에서 호출해야 traceparent 가 inject 됨.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/ssabro/rosshield/internal/platform/httpclient"
	"github.com/ssabro/rosshield/internal/platform/llm"
	"github.com/ssabro/rosshield/internal/platform/llm/anthropic"
	platformotel "github.com/ssabro/rosshield/internal/platform/otel"
)

func TestAnthropic_OtelTransport_InjectsTraceparent(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("traceparent")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	provider := platformotel.NewProviderFromComponents(tp, prop, true)

	wrapped := httpclient.WrapClient(&http.Client{}, provider, "llm-anthropic")
	a := anthropic.New(anthropic.Options{
		APIKey:     "k",
		BaseURL:    srv.URL,
		HTTPClient: wrapped,
	})

	// active span 이 있는 ctx 에서 호출해야 traceparent 가 inject 됨.
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test.parent")
	defer span.End()

	if _, err := a.Complete(ctx, llm.CompleteRequest{Model: "claude-3-haiku-20240307"}); err != nil {
		t.Fatalf("err=%v", err)
	}
	if captured == "" {
		t.Fatalf("traceparent header missing on outbound request")
	}
}

// TestAnthropic_NilHTTPClient_FallsBackToDefault 는 Options.HTTPClient 미주입 시
// 기본 client 가 사용됨을 검증 (기존 호출자 회귀 0).
func TestAnthropic_NilHTTPClient_FallsBackToDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()

	a := anthropic.New(anthropic.Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := a.Complete(context.Background(), llm.CompleteRequest{Model: "claude-3-haiku-20240307"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content=%q", resp.Content)
	}
}
