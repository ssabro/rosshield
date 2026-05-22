package otel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
)

// newExporter 는 Config.ExporterType 에 따라 OTLP exporter 를 생성합니다 (D-P11A-2).
//
//	grpc/"" → otlptracegrpc (권장 default — streaming 효율)
//	http   → otlptracehttp (corporate proxy / HTTP only 환경 cover)
//
// Insecure=true 면 TLS 미사용 — air-gap 자체 collector 또는 dev 환경에서만 권장.
// Headers 가 존재하면 OTLP collector 인증 header 로 전달 (Bearer · Datadog API key).
func newExporter(ctx context.Context, cfg Config) (*otlptrace.Exporter, error) {
	switch cfg.ExporterType {
	case "", ExporterGRPC, "otlp-grpc":
		return newGRPCExporter(ctx, cfg)
	case ExporterHTTP, "otlp-http":
		return newHTTPExporter(ctx, cfg)
	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidExporterType, cfg.ExporterType)
	}
}

func newGRPCExporter(ctx context.Context, cfg Config) (*otlptrace.Exporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
	}
	return otlptracegrpc.New(ctx, opts...)
}

func newHTTPExporter(ctx context.Context, cfg Config) (*otlptrace.Exporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
	}
	return otlptracehttp.New(ctx, opts...)
}
