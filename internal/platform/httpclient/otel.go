// Package httpclient 은 outbound HTTP 호출의 OpenTelemetry trace propagation 을
// 결선하는 helper 입니다 (Phase 11.A-5).
//
// 책임:
//   - http.Client 의 Transport 를 otelhttp.NewTransport 로 wrap → outgoing
//     `traceparent` W3C header 자동 inject (downstream cross-region/cross-service).
//   - target attribute(rosshield.outbound.target = "patroni"|"webhook"|"llm" 등)
//     를 매 request 의 span 에 부착해 dashboard 에서 outbound target 별 분류.
//   - noop provider (Enabled=false) 시 wrap 자체를 short-circuit — 원본 transport
//     를 그대로 반환해 overhead 0.
//
// 도메인 경계:
//   - 본 패키지는 platform 계층 — domain · application 어디든 import 가능.
//   - otelhttp 의존을 단일 곳에 집중 → patroni/webhook/LLM 4 provider 의 wrap
//     코드 중복 회피.
//
// 결정 항목 추적:
//   - D-P11A-1 = 옵션 A (otel SDK 전면) — 본 helper 가 outbound 측 결선.
//   - W3C trace context standard (otelhttp 가 자동 inject) — Patroni REST · webhook
//     수신자 모두 표준 호환.
package httpclient

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	platformotel "github.com/ssabro/rosshield/internal/platform/otel"
)

// outboundTargetAttr 은 outbound request span 에 부착되는 target 식별 attribute key 입니다.
//
// 값 예시: "patroni" · "webhook" · "llm" · "anchor". customer dashboard 에서 outbound
// 호출을 target 별로 분류해 SLO/latency 분석.
const outboundTargetAttr = "rosshield.outbound.target"

// WrapTransport 는 base transport 를 otelhttp 로 wrap 해 outgoing `traceparent`
// header 가 자동 inject 되도록 합니다 (Phase 11.A-5).
//
// 동작:
//   - tp == nil 또는 tp.Enabled() == false → base 를 그대로 반환 (overhead 0).
//   - 그 외 → otelhttp.NewTransport(base, options...) 반환.
//     options:
//   - WithTracerProvider(tp.TracerProvider())
//   - WithPropagators(tp.Propagator())
//   - WithSpanNameFormatter — "HTTP <METHOD> <target>" 형식
//   - WithSpanOptions — outbound target attribute 부착
//
// base 가 nil 이면 http.DefaultTransport 사용.
func WrapTransport(base http.RoundTripper, tp *platformotel.Provider, target string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if tp == nil || !tp.Enabled() {
		return base
	}
	return otelhttp.NewTransport(
		base,
		otelhttp.WithTracerProvider(tp.TracerProvider()),
		otelhttp.WithPropagators(tp.Propagator()),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			if target == "" {
				return "HTTP " + r.Method
			}
			return "HTTP " + r.Method + " " + target
		}),
		otelhttp.WithSpanOptions(trace.WithAttributes(
			attribute.String(outboundTargetAttr, target),
		)),
	)
}

// WrapClient 는 http.Client 의 Transport 를 WrapTransport 로 wrap 합니다.
//
// 동작:
//   - client == nil → 새 http.Client 를 만들어 반환 (Transport 만 wrap).
//   - 그 외 → 기존 client 의 다른 field(Timeout · CheckRedirect · Jar) 는 그대로 보존,
//     Transport 만 wrap.
//
// 호출자가 원본 client 를 계속 사용하지 않도록 새 인스턴스를 반환 — concurrent
// mutation 회피.
func WrapClient(client *http.Client, tp *platformotel.Provider, target string) *http.Client {
	if client == nil {
		return &http.Client{
			Transport: WrapTransport(nil, tp, target),
		}
	}
	if tp == nil || !tp.Enabled() {
		return client
	}
	cp := *client
	cp.Transport = WrapTransport(client.Transport, tp, target)
	return &cp
}

// ExtractCarrier 는 http.Request.Header 를 propagation.HeaderCarrier 로 wrap 합니다.
//
// 단위 테스트에서 outgoing `traceparent` header 가 inject 되었는지 검증할 때 사용.
// production code 는 otelhttp 가 자동으로 처리하므로 본 helper 가 필요 없습니다.
func ExtractCarrier(h http.Header) propagation.HeaderCarrier {
	return propagation.HeaderCarrier(h)
}
