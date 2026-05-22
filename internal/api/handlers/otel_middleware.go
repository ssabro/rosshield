package handlers

// otel_middleware.go — Phase 11.A-3 HTTP request trace + W3C traceparent propagation.
//
// 책임:
//   - 모든 /api/v1/* request 마다 server span 생성 (chi route pattern 기반 span name).
//   - W3C `traceparent` header 추출(incoming) + outgoing response header 에 명시.
//   - tenant.id · actor.id · http.method · http.route · http.status_code 등 attribute 부착.
//   - response 5xx 시 span status=Error, 4xx 는 보통 클라이언트 오류이므로 OK 유지.
//   - logger keyTraceID 자동 inject — slog log line ↔ trace UI cross-reference.
//
// 도메인 경계:
//   - 본 middleware 는 internal/api 계층 (Stage 11.A-3). domain · storage 직접 접근 0.
//   - tenant 추출은 storage.TenantIDFromContext(ctx) 활용 (AuthMiddleware 가 미리 inject).
//   - actor 추출은 claimsFromContext 활용 (동일 패키지 unexported helper).
//
// noop 일관:
//   - platformotel.Provider.Enabled()=false 면 모든 호출이 short-circuit — middleware 가
//     next.ServeHTTP 만 호출하고 즉시 return. overhead ≈ 0.
//   - global noop tracer 가 ctx 의 trace ID 를 emit 하지 않으므로 logger traceId attr 도
//     자동 미부착 (logger contextHandler 가 빈 문자열 skip).
//
// 결정 항목 추적:
//   - D-P11A-1 = 옵션 A (otel SDK 전면).
//   - D-P11A-3 = parent_based + ratio 0.05 — 본 middleware 는 모든 request 에 Start 호출,
//     실 sampling 은 TracerProvider 의 sampler 가 결정.
//   - chi RoutePattern 기반 attribute — high-cardinality URL path 폭주 회피.
//   - W3C trace context 표준 일관 (otelhttp 의 propagator wrap 과 동일 동작, 자체 wrap
//     으로 chi route attribute 추가 + 코드 양 작음).

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/ssabro/rosshield/internal/platform/logger"
	platformotel "github.com/ssabro/rosshield/internal/platform/otel"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// otelTracerScope 는 본 middleware 가 생성하는 span 의 instrumentation scope 입니다.
const otelTracerScope = "rosshield/api/handlers"

// attribute keys — OpenTelemetry semantic conventions (HTTP) + rosshield 커스텀.
// W3C trace context header (`traceparent` · `tracestate`) 추출/inject 은 모두
// propagation.HeaderCarrier 위임 — 명시 const 미사용.
const (
	attrHTTPMethod        = "http.method"
	attrHTTPRoute         = "http.route"
	attrHTTPStatusCode    = "http.status_code"
	attrHTTPURL           = "http.url"
	attrHTTPTarget        = "http.target"
	attrRosshieldTenantID = "rosshield.tenant_id"
	attrRosshieldActorID  = "rosshield.actor_id"
)

// OtelTrace 는 모든 HTTP request 에 대해 server span 을 emit 하는 chi middleware 입니다.
//
// 동작 (Enabled 일 때):
//  1. incoming `traceparent` header 를 global TextMapPropagator 로 parse → parent ctx.
//  2. tracer.Start(ctx, placeholder name) — defer span.End().
//  3. SetTraceIDOnContext — logger 의 keyTraceID 에 trace_id 자동 inject.
//  4. next.ServeHTTP 호출 (status capture 위해 statusRecorder wrap).
//  5. chi RoutePattern() 으로 span name 갱신 ("HTTP <METHOD> <route>") + http.route attribute.
//  6. tenant/actor attribute 부착 (가능할 때).
//  7. status code → span status (5xx = Error).
//  8. response header 에 outgoing `traceparent` inject (downstream cross-service).
//
// Enabled=false 또는 tp=nil → no-op (overhead 거의 0).
func OtelTrace(tp *platformotel.Provider) func(http.Handler) http.Handler {
	if tp == nil || !tp.Enabled() {
		return func(next http.Handler) http.Handler { return next }
	}
	tracer := tp.Tracer(otelTracerScope)
	propagator := tp.Propagator()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handleOtelRequest(w, r, tracer, propagator, next)
		})
	}
}

// handleOtelRequest 는 OtelTrace 의 per-request hot path 입니다 (함수 길이 제한 일관).
func handleOtelRequest(
	w http.ResponseWriter,
	r *http.Request,
	tracer trace.Tracer,
	propagator propagation.TextMapPropagator,
	next http.Handler,
) {
	// 1) incoming W3C traceparent parse → parent span context.
	ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

	// 2) span 시작 — name 은 placeholder, RoutePattern 확보 후 갱신.
	spanName := "HTTP " + r.Method
	ctx, span := tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String(attrHTTPMethod, r.Method),
			attribute.String(attrHTTPURL, r.URL.String()),
			attribute.String(attrHTTPTarget, r.URL.Path),
		),
	)
	defer span.End()

	// 3) logger keyTraceID inject — slog log ↔ trace UI cross-reference.
	ctx = platformotel.SetTraceIDOnContext(ctx)
	r = r.WithContext(ctx)

	// 4) outgoing response 에 traceparent inject (cross-service downstream).
	//    응답 status code 작성 *전* 에 header 설정해야 chi/stdlib http 가 전송.
	propagator.Inject(ctx, propagation.HeaderCarrier(w.Header()))

	// 5) status recorder wrap — WriteHeader 가호출되지 않은 경우 200 default.
	rec := &otelStatusRecorder{ResponseWriter: w, status: http.StatusOK}
	next.ServeHTTP(rec, r)

	// 6) post-handler attribute 갱신 — RoutePattern 은 next 실행 *후* 만 안정.
	finalizeOtelSpan(r, span, rec.status)
}

// finalizeOtelSpan 은 handler 종료 후 span 의 name + attribute + status 를 갱신합니다.
func finalizeOtelSpan(r *http.Request, span trace.Span, status int) {
	// chi RoutePattern — handler 매칭 후 안정. 매칭 실패(404) 면 빈 문자열 → skip.
	route := ""
	if rc := chi.RouteContext(r.Context()); rc != nil {
		route = rc.RoutePattern()
	}
	if route != "" {
		span.SetName("HTTP " + r.Method + " " + route)
		span.SetAttributes(attribute.String(attrHTTPRoute, route))
	}

	// tenant / actor attribute — AuthMiddleware 통과 후 ctx 에 채워짐.
	if tid := storage.TenantIDFromContext(r.Context()); tid != "" {
		span.SetAttributes(attribute.String(attrRosshieldTenantID, string(tid)))
	}
	if claims, ok := claimsFromContext(r.Context()); ok && claims.Subject != "" {
		span.SetAttributes(attribute.String(attrRosshieldActorID, claims.Subject))
	}

	// HTTP status → span status. 5xx = Error, 그 외 OK (Unset 유지).
	span.SetAttributes(attribute.Int(attrHTTPStatusCode, status))
	if status >= 500 {
		span.SetStatus(codes.Error, http.StatusText(status))
	}

	// requestId 가 ctx 에 있으면 추가 attribute (logger 와 cross-reference).
	if rid := logger.RequestID(r.Context()); rid != "" {
		span.SetAttributes(attribute.String("rosshield.request_id", rid))
	}
}

// otelStatusRecorder 는 http.ResponseWriter wrapper 로 status code 를 캡처합니다.
//
// chi.NewRouter 가 자체 wrap 하지 않아 본 middleware 가 status capture 부담.
// WriteHeader 호출 전이면 default 200 — net/http 표준 일관.
type otelStatusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *otelStatusRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *otelStatusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		// net/http 의 implicit WriteHeader(200) 일관.
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}

// Flush 는 streaming response (SSE · long-poll) 호환을 위해 위임합니다.
func (r *otelStatusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
