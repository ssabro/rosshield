// Package otel는 OpenTelemetry tracer provider scaffold입니다 (Phase 11.A-2).
//
// 본 패키지는 platform 계층에 위치하며 도메인 import 0 — domain 패키지가
// "go.opentelemetry.io/otel/trace" 를 직접 import 해 span 을 emit 하는 식으로
// 분리되어 있습니다(원칙 §5 도메인 경계).
//
// 운영 default = Enabled=false (opt-in, R14-1 원칙 일관). customer 가 명시적으로
// `--otel-enabled=true` 또는 env `ROSSHIELD_OTEL_ENABLED=true` 로 활성화할 때만
// 실 exporter 가 연결되며, 그 외에는 noop tracer 만 반환합니다.
//
// 결정 항목 추적:
//   - D-P11A-1 = 옵션 A (otel SDK 전면)
//   - D-P11A-2 = otlp-grpc + otlp-http both (Config.ExporterType 분기)
//   - D-P11A-3 = parent_based(traceidratiobased(0.05)) default
package otel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// DefaultServiceName 은 service.name resource attribute 의 기본값입니다.
const DefaultServiceName = "rosshield"

// DefaultSamplingRatio 는 D-P11A-3 의 권장 root sampling 비율(5%)입니다.
//
// 사용자 round 결정 = parent_based(traceidratiobased(0.05)) — root span 5% +
// child span 은 parent 결정 상속. 이 값은 [0.0, 1.0] 범위에서만 의미가 있으며
// 그 외 값은 NewSampler 에서 정규화됩니다.
const DefaultSamplingRatio = 0.05

// DefaultShutdownTimeout 은 Provider.Shutdown 의 최대 대기 시간입니다.
//
// 호출자가 context.WithTimeout 으로 더 짧은 timeout 을 명시하면 그쪽이 우선.
const DefaultShutdownTimeout = 5 * time.Second

// ExporterType 은 OTLP exporter transport 선택자입니다 (D-P11A-2).
//
// "" 또는 "grpc" 또는 "otlp-grpc" → gRPC transport (권장 default).
// "http" 또는 "otlp-http"          → HTTP/protobuf transport.
//
// 그 외 값은 NewProvider 에서 ErrInvalidExporterType 으로 거부합니다.
type ExporterType string

const (
	// ExporterGRPC 는 otlptracegrpc transport 입니다.
	ExporterGRPC ExporterType = "grpc"
	// ExporterHTTP 는 otlptracehttp transport 입니다.
	ExporterHTTP ExporterType = "http"
)

// ErrInvalidExporterType 은 Config.ExporterType 이 grpc/http 외 값일 때 반환됩니다.
var ErrInvalidExporterType = errors.New("otel: invalid exporter type (allowed: grpc|http)")

// ErrEndpointRequired 는 Enabled=true 이지만 Config.Endpoint 가 빈 값일 때 반환됩니다.
var ErrEndpointRequired = errors.New("otel: endpoint is required when enabled")

// Config 는 NewProvider 의 입력 구성입니다.
//
// 모든 field 의 zero value 는 production-safe — Enabled=false 면 다른 field 는
// 무시되고 noop provider 가 반환됩니다. customer 가 활성화할 때만 Endpoint 등
// validation 이 발동합니다.
type Config struct {
	// Enabled = false (default) 면 NewProvider 는 noop tracer 만 반환합니다.
	// true 면 Endpoint + ExporterType 이 필수 — validation 실패 시 부트스트랩 에러.
	Enabled bool

	// ServiceName 은 OpenTelemetry resource 의 service.name attribute. 빈 값이면
	// DefaultServiceName("rosshield") 적용. customer 가 multi-tenant 배포 시
	// "rosshield-<env>" 등 환경별 식별자로 override 가능.
	ServiceName string

	// ServiceVersion 은 service.version attribute. 빈 값이면 attribute 미부착.
	// 보통 BuildVersion("v0.13.x") 또는 git SHA 를 주입.
	ServiceVersion string

	// Endpoint 는 OTLP collector 의 host:port 입니다.
	//
	//   gRPC: "localhost:4317" 또는 "otel-collector.default.svc:4317"
	//   HTTP: "localhost:4318" 또는 full URL "https://otel.example.com:4318"
	//
	// Enabled=true 시 빈 값이면 ErrEndpointRequired.
	Endpoint string

	// ExporterType 은 OTLP transport 선택 (D-P11A-2). 빈 값이면 grpc (권장 default).
	ExporterType ExporterType

	// Insecure = true 면 TLS 미사용 (gRPC 의 WithInsecure, HTTP 의 WithInsecure).
	// production 권장 false. air-gap 자체 collector 또는 dev 환경만 true.
	Insecure bool

	// SamplingRatio 는 root span sampling 비율 (D-P11A-3).
	//
	//   <= 0   → NeverSample (모든 span drop)
	//   >= 1.0 → AlwaysSample (모든 span emit)
	//   그 외  → parent_based(traceidratiobased(ratio))
	//
	// 빈 Config 의 default(0)는 NeverSample — production-safe 보수적.
	// customer 가 명시적으로 0.05 등을 지정해야 emit 시작.
	SamplingRatio float64

	// Headers 는 OTLP collector 인증용 추가 header 입니다 (Bearer/Datadog 등).
	// 빈 map 이면 무처리.
	Headers map[string]string

	// Region 은 multi-region 배포 시 rosshield.region resource attribute. 빈 값이면
	// attribute 미부착. Phase 8 multi-region HA 와 cross-reference 용.
	Region string

	// Environment 는 deployment.environment attribute ("production"|"staging"|"dev" 등).
	// 빈 값이면 attribute 미부착.
	Environment string
}

// Provider 는 TracerProvider 와 Shutdown 의 묶음입니다.
//
// NewProvider 가 항상 non-nil Provider 를 반환 — Enabled=false 인 경우에도 noop
// tracer 가 내부에서 사용되어 호출자는 nil 체크 없이 동일 인터페이스를 사용할 수
// 있습니다.
type Provider struct {
	tp         trace.TracerProvider
	propagator propagation.TextMapPropagator
	shutdownFn func(context.Context) error
	enabled    bool
}

// NewProvider 는 Config 로부터 TracerProvider 를 생성합니다.
//
// Enabled=false → noop provider (실 exporter 미연결, 호출 비용 거의 0).
// Enabled=true  → OTLP exporter + parent_based sampler + resource attribute 결선.
//
// 부트스트랩 에러 시 noop provider 반환 X — 호출자가 fail-fast 결정.
// global TracerProvider 와 TextMapPropagator 는 모두 본 호출에서 set 됩니다.
func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	if !cfg.Enabled {
		p := newNoopProvider()
		// global propagator 는 noop 일 때도 설정 — Stage 11.A-3 에서 incoming
		// W3C traceparent header parse 시 의도하지 않은 panic 회피.
		otel.SetTextMapPropagator(p.propagator)
		otel.SetTracerProvider(p.tp)
		return p, nil
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, ErrEndpointRequired
	}

	exporter, err := newExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("otel: build exporter: %w", err)
	}

	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("otel: build resource: %w", err)
	}

	sampler := NewSampler(cfg.SamplingRatio)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(prop)

	return &Provider{
		tp:         tp,
		propagator: prop,
		shutdownFn: tp.Shutdown,
		enabled:    true,
	}, nil
}

// Tracer 는 본 provider 가 관리하는 Tracer 를 반환합니다.
//
// scope 는 instrument 위치 식별자(예: "rosshield/api/handlers" · "rosshield/scanrun").
// 빈 문자열이면 service.name 기준 default 사용.
func (p *Provider) Tracer(scope string) trace.Tracer {
	if p == nil {
		return tracenoop.NewTracerProvider().Tracer("")
	}
	if scope == "" {
		scope = DefaultServiceName
	}
	return p.tp.Tracer(scope)
}

// TracerProvider 는 내부 TracerProvider 인스턴스를 노출합니다.
//
// otelhttp.NewMiddleware 등 외부 helper 가 명시적으로 provider 를 받기를 원할 때 사용.
func (p *Provider) TracerProvider() trace.TracerProvider {
	if p == nil {
		return tracenoop.NewTracerProvider()
	}
	return p.tp
}

// Propagator 는 본 provider 가 등록한 TextMapPropagator 를 반환합니다 (W3C + baggage).
func (p *Provider) Propagator() propagation.TextMapPropagator {
	if p == nil {
		return propagation.NewCompositeTextMapPropagator()
	}
	return p.propagator
}

// Enabled 는 실 exporter 가 연결되어 있는지 여부를 반환합니다.
//
// false 면 모든 Tracer 호출은 noop (span emit 없음). 진단/테스트에서 사용.
func (p *Provider) Enabled() bool {
	if p == nil {
		return false
	}
	return p.enabled
}

// Shutdown 은 pending span 을 flush 한 뒤 exporter 를 닫습니다.
//
// ctx 가 deadline 을 가지면 그 deadline 까지만 flush 시도 — production graceful
// shutdown 흐름과 호환. noop provider 는 즉시 nil 반환.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.shutdownFn == nil {
		return nil
	}
	return p.shutdownFn(ctx)
}

// newNoopProvider 는 Enabled=false 시 반환되는 short-circuit provider 입니다.
//
// global TracerProvider 가 NoopTracerProvider 가 되며 모든 trace.Span 호출은
// 호출 비용이 거의 0 입니다 (struct{} 반환). propagator 도 비어 있어 incoming
// header 는 무시.
func newNoopProvider() *Provider {
	return &Provider{
		tp:         tracenoop.NewTracerProvider(),
		propagator: propagation.NewCompositeTextMapPropagator(),
		shutdownFn: func(context.Context) error { return nil },
		enabled:    false,
	}
}
