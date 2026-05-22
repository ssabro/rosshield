package otel

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/ssabro/rosshield/internal/platform/logger"
)

func TestNewProvider_DisabledReturnsNoop(t *testing.T) {
	t.Parallel()

	p, err := NewProvider(context.Background(), Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider(disabled) returned err: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider for disabled config")
	}
	if p.Enabled() {
		t.Fatal("disabled provider reports Enabled()=true")
	}

	tr := p.Tracer("test")
	if tr == nil {
		t.Fatal("Tracer must not be nil")
	}

	// noop span 은 IsValid()=false — emit 안 됨.
	_, span := tr.Start(context.Background(), "noop-span")
	if span.SpanContext().IsValid() {
		t.Fatal("noop tracer produced a valid SpanContext (unexpected)")
	}
	span.End()

	// Shutdown 은 즉시 nil 반환.
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown(noop) returned err: %v", err)
	}
}

func TestNewProvider_EnabledRequiresEndpoint(t *testing.T) {
	t.Parallel()

	_, err := NewProvider(context.Background(), Config{Enabled: true, ExporterType: ExporterGRPC})
	if !errors.Is(err, ErrEndpointRequired) {
		t.Fatalf("expected ErrEndpointRequired, got %v", err)
	}
}

func TestNewProvider_EnabledRejectsUnknownExporterType(t *testing.T) {
	t.Parallel()

	_, err := NewProvider(context.Background(), Config{
		Enabled:      true,
		Endpoint:     "127.0.0.1:4317",
		ExporterType: "carrier-pigeon",
	})
	if !errors.Is(err, ErrInvalidExporterType) {
		t.Fatalf("expected ErrInvalidExporterType, got %v", err)
	}
}

func TestNewProvider_EnabledWithGRPCExporter(t *testing.T) {
	t.Parallel()

	// 실제 OTLP collector 에 연결하지 않음 — listen 만 하는 TCP socket 으로 endpoint 만족.
	// otlptracegrpc 는 lazy dial 이라 listen 만으로 New() 가 성공.
	addr := startListenOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p, err := NewProvider(ctx, Config{
		Enabled:        true,
		ServiceName:    "rosshield-test",
		ServiceVersion: "v0.0.0-test",
		Endpoint:       addr,
		ExporterType:   ExporterGRPC,
		Insecure:       true,
		SamplingRatio:  1.0, // 본 test 는 emit 확인이므로 AlwaysSample.
		Region:         "test-region",
		Environment:    "test",
	})
	if err != nil {
		t.Fatalf("NewProvider(enabled) returned err: %v", err)
	}
	t.Cleanup(func() {
		ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelShutdown()
		_ = p.Shutdown(ctxShutdown)
	})

	if !p.Enabled() {
		t.Fatal("enabled provider reports Enabled()=false")
	}

	// span 1 건 emit — 유효한 SpanContext 가 생성되는지 확인 (실 export 는 BatchSpanProcessor 에 위임).
	tr := p.Tracer("rosshield/test")
	_, span := tr.Start(context.Background(), "smoke")
	if !span.SpanContext().IsValid() {
		t.Fatal("expected valid SpanContext from enabled provider")
	}
	span.End()
}

func TestProvider_ShutdownIsIdempotent(t *testing.T) {
	t.Parallel()

	p, err := NewProvider(context.Background(), Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := p.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown #%d: %v", i, err)
		}
	}
}

func TestNewSampler_RatioBoundary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		ratio float64
	}{
		{"never", -0.1},
		{"never_zero", 0},
		{"ratio_5pct", 0.05},
		{"ratio_default", DefaultSamplingRatio},
		{"always_one", 1.0},
		{"always_over", 1.5},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewSampler(tc.ratio)
			if s == nil {
				t.Fatal("NewSampler returned nil")
			}
			// Description 만 비어있지 않음을 확인 — sampler 종류는 SDK 내부 enum.
			if s.Description() == "" {
				t.Fatal("sampler Description is empty")
			}
		})
	}
}

func TestSetTraceIDOnContext_NoSpanReturnsSameCtx(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	out := SetTraceIDOnContext(ctx)
	if logger.TraceID(out) != "" {
		t.Fatalf("expected empty trace ID for ctx without span, got %q", logger.TraceID(out))
	}
}

func TestSetTraceIDOnContext_PropagatesSpanTraceID(t *testing.T) {
	t.Parallel()

	// 자체 TracerProvider 로 valid span 을 만든 뒤 ctx 에 부착해 검증
	// (global TracerProvider 오염 회피).
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	tr := tp.Tracer("test")
	ctx, span := tr.Start(context.Background(), "unit")
	defer span.End()

	out := SetTraceIDOnContext(ctx)
	got := logger.TraceID(out)
	if got == "" {
		t.Fatal("expected non-empty trace ID in ctx after SetTraceIDOnContext")
	}

	expect := trace.SpanFromContext(ctx).SpanContext().TraceID().String()
	if got != expect {
		t.Fatalf("trace ID mismatch: ctx=%q span=%q", got, expect)
	}

	// TraceIDFromContext helper 도 동일 값.
	if got := TraceIDFromContext(ctx); got != expect {
		t.Fatalf("TraceIDFromContext mismatch: got %q want %q", got, expect)
	}
}

func TestTraceIDFromContext_NoSpanReturnsEmpty(t *testing.T) {
	t.Parallel()
	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty trace ID, got %q", got)
	}
}

// startListenOnly 는 임의 포트에 listen 만 하는 TCP socket 을 만들어 endpoint
// 문자열을 반환합니다. otlptracegrpc/http 가 lazy-dial 이므로 listen 만으로 New()
// 호출은 성공 — 실 connection 은 첫 export 시도 시.
func startListenOnly(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l.Addr().String()
}
