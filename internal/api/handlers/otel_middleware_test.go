package handlers

// otel_middleware_test.go — Phase 11.A-3 단위 테스트.
//
// cover:
//   - noop provider 시 middleware no-op (회귀 0)
//   - incoming W3C traceparent header parse + parent ctx propagation
//   - outgoing response header 의 traceparent emit
//   - span name "HTTP <METHOD> <route>" 정확 (chi RoutePattern)
//   - http.method/route/status_code attribute 정확
//   - 5xx response 시 span status = Error
//   - 4xx response 시 span status = Unset (클라이언트 오류)
//   - tenant/actor attribute 부착 (AuthMiddleware 후속 ctx)

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ssabro/rosshield/internal/domain/tenant"
	platformotel "github.com/ssabro/rosshield/internal/platform/otel"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// newOtelTestProvider 는 in-memory SpanRecorder 가 부착된 Provider 를 반환합니다.
func newOtelTestProvider(t *testing.T) (*platformotel.Provider, *tracetest.SpanRecorder) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	return platformotel.NewProviderFromComponents(tp, propagator, true), sr
}

// chiRouterWith 는 OtelTrace 미들웨어 + 주어진 route pattern + handler 로 chi 라우터를 구성합니다.
func chiRouterWith(tp *platformotel.Provider, pattern string, handler http.HandlerFunc) chi.Router {
	r := chi.NewRouter()
	r.Use(OtelTrace(tp))
	r.Get(pattern, handler)
	r.Post(pattern, handler)
	return r
}

func TestOtelTrace_NoopProvider_NoSpans(t *testing.T) {
	t.Parallel()

	// nil provider — middleware 가 short-circuit. handler 정상 호출.
	r := chiRouterWith(nil, "/api/v1/robots", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/robots", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nil provider middleware broke handler: status=%d", rec.Code)
	}
	if got := rec.Header().Get("traceparent"); got != "" {
		t.Fatalf("nil provider must not emit traceparent, got %q", got)
	}

	// Enabled=false provider — 동일.
	disabled, err := platformotel.NewProvider(context.Background(), platformotel.Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider(disabled): %v", err)
	}
	r2 := chiRouterWith(disabled, "/api/v1/robots", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec2 := httptest.NewRecorder()
	r2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/v1/robots", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("disabled provider broke handler: status=%d", rec2.Code)
	}
	if got := rec2.Header().Get("traceparent"); got != "" {
		t.Fatalf("disabled provider must not emit traceparent, got %q", got)
	}
}

func TestOtelTrace_EmitsSpanWithRoutePattern(t *testing.T) {
	t.Parallel()

	tp, sr := newOtelTestProvider(t)
	r := chiRouterWith(tp, "/api/v1/robots/{robotId}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/robots/rb_xyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	span := spans[0]
	wantName := "HTTP GET /api/v1/robots/{robotId}"
	if span.Name() != wantName {
		t.Fatalf("span name mismatch: got %q want %q", span.Name(), wantName)
	}
	assertAttr(t, span.Attributes(), attrHTTPMethod, "GET")
	assertAttr(t, span.Attributes(), attrHTTPRoute, "/api/v1/robots/{robotId}")
	assertAttr(t, span.Attributes(), attrHTTPStatusCode, int64(200))
	if span.Status().Code != codes.Unset {
		t.Fatalf("expected Unset status for 2xx, got %v", span.Status().Code)
	}
}

func TestOtelTrace_IncomingTraceparentPropagates(t *testing.T) {
	t.Parallel()

	tp, sr := newOtelTestProvider(t)

	// W3C traceparent: version-traceid-spanid-flags
	parentTraceID := "0af7651916cd43dd8448eb211c80319c"
	parentSpanID := "b7ad6b7169203331"
	incoming := "00-" + parentTraceID + "-" + parentSpanID + "-01"

	var capturedTraceID string
	r := chiRouterWith(tp, "/api/v1/scans", func(w http.ResponseWriter, req *http.Request) {
		capturedTraceID = platformotel.TraceIDFromContext(req.Context())
		w.WriteHeader(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scans", strings.NewReader(""))
	req.Header.Set("traceparent", incoming)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if capturedTraceID != parentTraceID {
		t.Fatalf("expected handler ctx trace ID %q, got %q", parentTraceID, capturedTraceID)
	}
	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].SpanContext().TraceID().String(); got != parentTraceID {
		t.Fatalf("server span did not inherit parent trace ID: got %q want %q", got, parentTraceID)
	}
	if got := spans[0].Parent().SpanID().String(); got != parentSpanID {
		t.Fatalf("server span parent SpanID mismatch: got %q want %q", got, parentSpanID)
	}
}

func TestOtelTrace_OutgoingTraceparentInjected(t *testing.T) {
	t.Parallel()

	tp, _ := newOtelTestProvider(t)
	r := chiRouterWith(tp, "/api/v1/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))

	got := rec.Header().Get("traceparent")
	if got == "" {
		t.Fatal("expected outgoing traceparent header, got empty")
	}
	// 형식 검증 — 4 부분으로 split, version=00, traceid 32hex, spanid 16hex, flags 2hex.
	parts := strings.Split(got, "-")
	if len(parts) != 4 {
		t.Fatalf("traceparent malformed: %q", got)
	}
	if parts[0] != "00" {
		t.Fatalf("traceparent version != 00: %q", parts[0])
	}
	if len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		t.Fatalf("traceparent component lengths wrong: %q", got)
	}
}

func TestOtelTrace_ServerErrorSetsErrorStatus(t *testing.T) {
	t.Parallel()

	tp, sr := newOtelTestProvider(t)
	r := chiRouterWith(tp, "/api/v1/fail", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/fail", nil))

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatalf("expected Error status for 5xx, got %v", spans[0].Status().Code)
	}
	assertAttr(t, spans[0].Attributes(), attrHTTPStatusCode, int64(500))
}

func TestOtelTrace_ClientErrorKeepsUnsetStatus(t *testing.T) {
	t.Parallel()

	tp, sr := newOtelTestProvider(t)
	r := chiRouterWith(tp, "/api/v1/forbidden", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forbidden", nil))

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status().Code != codes.Unset {
		t.Fatalf("expected Unset status for 4xx, got %v", spans[0].Status().Code)
	}
	assertAttr(t, spans[0].Attributes(), attrHTTPStatusCode, int64(403))
}

func TestOtelTrace_TenantAndActorAttributes(t *testing.T) {
	t.Parallel()

	tp, sr := newOtelTestProvider(t)

	// AuthMiddleware 흉내 — claims + tenantID 를 ctx 에 주입한 뒤 OtelTrace 통과.
	authShim := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := tenant.AccessClaims{
				Subject:  "us_test",
				TenantID: storage.TenantID("tn_acme"),
			}
			ctx := context.WithValue(r.Context(), claimsCtxKey, claims)
			ctx = storage.WithTenantID(ctx, claims.TenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	r := chi.NewRouter()
	r.Use(authShim)      // 먼저 claims 주입
	r.Use(OtelTrace(tp)) // 그 후 trace
	r.Get("/api/v1/audit/head", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/audit/head", nil))

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	assertAttr(t, spans[0].Attributes(), attrRosshieldTenantID, "tn_acme")
	assertAttr(t, spans[0].Attributes(), attrRosshieldActorID, "us_test")
}

// assertAttr 는 span attribute 슬라이스에서 key=want 를 검증합니다.
func assertAttr(t *testing.T, attrs []attribute.KeyValue, key string, want any) {
	t.Helper()
	for _, kv := range attrs {
		if string(kv.Key) != key {
			continue
		}
		switch w := want.(type) {
		case string:
			if got := kv.Value.AsString(); got != w {
				t.Fatalf("attr %q: got %q want %q", key, got, w)
			}
		case int64:
			if got := kv.Value.AsInt64(); got != w {
				t.Fatalf("attr %q: got %d want %d", key, got, w)
			}
		default:
			t.Fatalf("unsupported want type for attr %q", key)
		}
		return
	}
	t.Fatalf("attribute %q not found in span", key)
}
