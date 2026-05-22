package ollama_test

// ollama_otel_test.go — Phase 11.A-6 outbound HTTP transport otel wrap 단위 검증.

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
	"github.com/ssabro/rosshield/internal/platform/llm/ollama"
	platformotel "github.com/ssabro/rosshield/internal/platform/otel"
)

func TestOllama_OtelTransport_InjectsTraceparent(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("traceparent")
		_, _ = io.WriteString(w, `{"response":"hi","done":true,"prompt_eval_count":1,"eval_count":1}`+"\n")
	}))
	defer srv.Close()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	provider := platformotel.NewProviderFromComponents(tp, prop, true)

	wrapped := httpclient.WrapClient(&http.Client{}, provider, "llm-ollama")
	a := ollama.New(ollama.Options{
		Endpoint:   srv.URL,
		HTTPClient: wrapped,
	})

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test.parent")
	defer span.End()

	if _, err := a.Complete(ctx, llm.CompleteRequest{Model: "llama3.2"}); err != nil {
		t.Fatalf("err=%v", err)
	}
	if captured == "" {
		t.Fatalf("traceparent header missing on outbound request")
	}
}
