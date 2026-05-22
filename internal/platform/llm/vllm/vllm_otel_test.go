package vllm_test

// vllm_otel_test.go — Phase 11.A-6 outbound HTTP transport otel wrap 단위 검증.

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
	"github.com/ssabro/rosshield/internal/platform/llm/vllm"
	platformotel "github.com/ssabro/rosshield/internal/platform/otel"
)

func TestVLLM_OtelTransport_InjectsTraceparent(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("traceparent")
		_, _ = io.WriteString(w, `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	provider := platformotel.NewProviderFromComponents(tp, prop, true)

	wrapped := httpclient.WrapClient(&http.Client{}, provider, "llm-vllm")
	a := vllm.New(vllm.Options{
		BaseURL:    srv.URL,
		HTTPClient: wrapped,
	})

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test.parent")
	defer span.End()

	if _, err := a.Complete(ctx, llm.CompleteRequest{Model: "any"}); err != nil {
		t.Fatalf("err=%v", err)
	}
	if captured == "" {
		t.Fatalf("traceparent header missing on outbound request")
	}
}
