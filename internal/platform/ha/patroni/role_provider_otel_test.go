package patroni

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ssabro/rosshield/internal/platform/httpclient"
	platformotel "github.com/ssabro/rosshield/internal/platform/otel"
)

// newRecordingProvider 는 in-memory SpanRecorder 를 부착한 provider 를 만듭니다.
func newRecordingProvider(t *testing.T) (*platformotel.Provider, *tracetest.SpanRecorder) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(rec),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	return platformotel.NewProviderFromComponents(tp, prop, true), rec
}

// TestPollOnce_EmitsClusterSpan 는 매 pollOnce 호출이 patroni.cluster span 을
// emit 하는지 + leader/timeline attribute 가 부착되는지 검증합니다.
func TestPollOnce_EmitsClusterSpan(t *testing.T) {
	t.Parallel()

	provider, rec := newRecordingProvider(t)

	srv, _ := newFakePatroni(t, &clusterResponse{
		Leader:   "pod-0",
		Timeline: 42,
		Members: []memberInfo{
			{Name: "pod-0", Role: "master", State: "running"},
		},
	})

	rp, err := New(Deps{
		PatroniURL:    srv.URL,
		LocalHostname: "pod-0",
		Logger:        discardLogger(),
		Tracer:        provider.Tracer("rosshield/platform/ha/patroni"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rp.pollOnce(context.Background())

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	span := spans[0]
	if span.Name() != "patroni.cluster" {
		t.Errorf("span.Name = %q, want patroni.cluster", span.Name())
	}

	attrs := map[attribute.Key]attribute.Value{}
	for _, a := range span.Attributes() {
		attrs[a.Key] = a.Value
	}

	if v := attrs[attribute.Key(attrPatroniLeader)].AsString(); v != "pod-0" {
		t.Errorf("leader attr = %q, want pod-0", v)
	}
	if v := attrs[attribute.Key(attrPatroniTimeline)].AsInt64(); v != 42 {
		t.Errorf("timeline attr = %d, want 42", v)
	}
	if v := attrs[attribute.Key(attrPatroniIsLeader)].AsBool(); !v {
		t.Errorf("is_leader attr = false, want true (pod-0 == leader)")
	}
	if v := attrs[attribute.Key(attrPatroniHTTPStatus)].AsInt64(); v != 200 {
		t.Errorf("http.status_code attr = %d, want 200", v)
	}
}

// TestPollOnce_SpanErrorOnHTTPFailure 는 HTTP 500 응답 시 span 이 error status 로
// 마감되는지 검증합니다.
func TestPollOnce_SpanErrorOnHTTPFailure(t *testing.T) {
	t.Parallel()

	provider, rec := newRecordingProvider(t)
	srv, _ := newFakePatroni(t, nil) // nil → 500 응답

	rp, err := New(Deps{
		PatroniURL:    srv.URL,
		LocalHostname: "pod-0",
		Logger:        discardLogger(),
		Tracer:        provider.Tracer("rosshield/platform/ha/patroni"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rp.pollOnce(context.Background())

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	span := spans[0]
	// codes.Error == 1
	if span.Status().Code != 1 {
		t.Errorf("span.Status.Code = %v, want Error(1)", span.Status().Code)
	}
}

// TestPollOnce_WithWrappedClient_InjectsTraceparent 는 httpclient.WrapClient 가
// 부착된 HTTPClient 를 사용 시 outgoing request 에 traceparent header 가 inject
// 되는지 검증합니다 (cross-region trace context propagation 의 end-to-end 검증).
func TestPollOnce_WithWrappedClient_InjectsTraceparent(t *testing.T) {
	t.Parallel()

	provider, _ := newRecordingProvider(t)

	var capturedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("Traceparent")
		// 정상 응답
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"leader":"pod-0","timeline":1,"members":[{"name":"pod-0","role":"master"}]}`))
	}))
	t.Cleanup(srv.Close)

	rp, err := New(Deps{
		PatroniURL:    srv.URL,
		LocalHostname: "pod-0",
		Logger:        discardLogger(),
		Tracer:        provider.Tracer("rosshield/platform/ha/patroni"),
		HTTPClient:    httpclient.WrapClient(nil, provider, "patroni"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rp.pollOnce(context.Background())

	if capturedHeader == "" {
		t.Fatal("traceparent header 미inject — cross-region propagation 실패")
	}
}

// TestPollOnce_NoopTracerOverheadZero 는 deps.Tracer=nil 일 때 noop tracer fallback
// 이 적용되어 span 호출이 no-op 인지 검증합니다 (R14-1 일관, overhead 0).
func TestPollOnce_NoopTracerOverheadZero(t *testing.T) {
	t.Parallel()

	srv, _ := newFakePatroni(t, &clusterResponse{Leader: "pod-0", Timeline: 1})
	rp, err := New(Deps{
		PatroniURL:    srv.URL,
		LocalHostname: "pod-0",
		Logger:        discardLogger(),
		// Tracer 미지정 → noop fallback
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// panic 안 함 + leader detection 정상.
	rp.pollOnce(context.Background())

	if !rp.IsLeader() {
		t.Error("noop tracer fallback 에서 leader detection 결함")
	}
}
