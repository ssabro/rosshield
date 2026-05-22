package httpclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	platformotel "github.com/ssabro/rosshield/internal/platform/otel"
)

// newRecordingProvider 는 in-memory SpanRecorder 가 부착된 platformotel.Provider 를
// 만듭니다 (Stage 11.A-5 test helper).
//
// enabled=true 로 합성 → WrapTransport / WrapClient 가 otelhttp wrap 분기 진입.
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

// TestWrapTransport_InjectsTraceparent 는 otelhttp wrap 된 transport 가 outgoing
// request 에 W3C `traceparent` header 를 자동 inject 하는지 검증합니다.
func TestWrapTransport_InjectsTraceparent(t *testing.T) {
	t.Parallel()

	provider, rec := newRecordingProvider(t)

	var capturedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("Traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{
		Transport: WrapTransport(http.DefaultTransport, provider, "patroni"),
	}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if capturedHeader == "" {
		t.Fatal("outbound request missing W3C traceparent header — otelhttp wrap 미적용")
	}
	if !strings.HasPrefix(capturedHeader, "00-") {
		t.Errorf("traceparent format unexpected: %q (W3C version 00 prefix 부재)", capturedHeader)
	}

	spans := rec.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least 1 outbound span recorded, got 0")
	}
	// outbound target attribute 부착 확인.
	found := false
	for _, s := range spans {
		for _, a := range s.Attributes() {
			if string(a.Key) == "rosshield.outbound.target" && a.Value.AsString() == "patroni" {
				found = true
			}
		}
	}
	if !found {
		t.Error("rosshield.outbound.target=patroni attribute 부착 안 됨")
	}
}

// TestWrapTransport_NoopProviderShortCircuits 는 Enabled=false provider 에서 base
// transport 가 그대로 반환되는지 검증합니다 (overhead 0).
func TestWrapTransport_NoopProviderShortCircuits(t *testing.T) {
	t.Parallel()

	noopProvider := platformotel.NewProviderFromComponents(nil, nil, false)
	base := http.DefaultTransport

	wrapped := WrapTransport(base, noopProvider, "patroni")
	if wrapped != base {
		t.Errorf("Enabled=false 에서 wrapped != base — short-circuit 실패")
	}
}

// TestWrapTransport_NilProvider 는 tp=nil 에서 base 가 그대로 반환되는지 검증합니다.
func TestWrapTransport_NilProvider(t *testing.T) {
	t.Parallel()

	base := http.DefaultTransport
	wrapped := WrapTransport(base, nil, "any")
	if wrapped != base {
		t.Errorf("tp=nil 에서 wrapped != base — short-circuit 실패")
	}
}

// TestWrapClient_PreservesTimeout 는 WrapClient 가 기존 client 의 다른 field 를
// 보존하는지 검증합니다 (Timeout · Jar 등 customer 환경 설정 유지).
func TestWrapClient_PreservesTimeout(t *testing.T) {
	t.Parallel()

	provider, _ := newRecordingProvider(t)

	base := &http.Client{Timeout: 7 * 1000 * 1000 * 1000} // 7s 임의 값
	wrapped := WrapClient(base, provider, "webhook")

	if wrapped == base {
		t.Error("WrapClient 가 동일 인스턴스 반환 — 새 인스턴스 반환해야 concurrent mutation 회피")
	}
	if wrapped.Timeout != base.Timeout {
		t.Errorf("Timeout 보존 안 됨: got %v, want %v", wrapped.Timeout, base.Timeout)
	}
}

// TestWrapClient_NilClient 는 client=nil 에서 새 http.Client 가 반환되는지 검증합니다.
func TestWrapClient_NilClient(t *testing.T) {
	t.Parallel()

	provider, _ := newRecordingProvider(t)
	wrapped := WrapClient(nil, provider, "llm")
	if wrapped == nil {
		t.Fatal("WrapClient(nil) returned nil")
	}
	if wrapped.Transport == nil {
		t.Error("Transport not wrapped — nil client path 결함")
	}
}

// TestWrapClient_NoopReturnsOriginal 는 Enabled=false 에서 동일 인스턴스 반환 검증.
func TestWrapClient_NoopReturnsOriginal(t *testing.T) {
	t.Parallel()

	noopProvider := platformotel.NewProviderFromComponents(nil, nil, false)
	base := &http.Client{}
	wrapped := WrapClient(base, noopProvider, "any")
	if wrapped != base {
		t.Error("Enabled=false 에서 WrapClient 가 새 인스턴스 반환 — overhead 0 위반")
	}
}
