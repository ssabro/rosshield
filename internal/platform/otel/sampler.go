package otel

import (
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// NewSampler 는 ratio 값에 따라 적절한 sampler 를 반환합니다 (D-P11A-3).
//
//	ratio <= 0   → NeverSample (모든 span drop, production-safe 보수적 default).
//	ratio >= 1.0 → AlwaysSample (모든 span emit, dev/디버깅 전용).
//	그 외        → ParentBased(TraceIDRatioBased(ratio))
//
// ParentBased 의 의미:
//   - root span: TraceIDRatioBased(ratio) 결정.
//   - child span: parent 의 sampling 결정 상속 (distributed 일관성 보장).
//
// 운영 권장 default = 0.05 (DefaultSamplingRatio) — root 5% sampling.
// 그 외 child span 은 parent 결정 그대로 — multi-hop 디버깅 일관성 보존.
func NewSampler(ratio float64) sdktrace.Sampler {
	switch {
	case ratio <= 0:
		return sdktrace.NeverSample()
	case ratio >= 1.0:
		return sdktrace.AlwaysSample()
	default:
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
	}
}
