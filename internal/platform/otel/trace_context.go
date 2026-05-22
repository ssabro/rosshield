package otel

import (
	"context"

	"github.com/ssabro/rosshield/internal/platform/logger"
	"go.opentelemetry.io/otel/trace"
)

// SetTraceIDOnContext 는 ctx 에 현재 활성 span 의 trace ID 를 logger keyTraceID
// ctx key 로 inject 합니다 (Stage 11.A-3 활용 예정).
//
// 동작:
//   - ctx 안 활성 span 의 SpanContext 가 valid 면 trace_id 16진 문자열을 logger
//     keyTraceID 에 부착해 contextHandler 가 자동으로 slog attr 에 emit.
//   - 활성 span 이 없거나 invalid 면 ctx 변경 없이 그대로 반환 (no-op).
//
// Stage 11.A-3 HTTP middleware 에서 W3C `traceparent` header parse 후 span 시작
// 시점에 호출 — slog log 와 trace UI 의 trace ID cross-reference 보장.
func SetTraceIDOnContext(ctx context.Context) context.Context {
	span := trace.SpanFromContext(ctx)
	sc := span.SpanContext()
	if !sc.IsValid() {
		return ctx
	}
	return logger.WithTraceID(ctx, sc.TraceID().String())
}

// TraceIDFromContext 는 ctx 의 활성 span 에서 trace ID 16진 문자열을 추출합니다.
//
// span 이 없거나 invalid 면 빈 문자열을 반환 — 호출자가 분기 결정.
func TraceIDFromContext(ctx context.Context) string {
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
