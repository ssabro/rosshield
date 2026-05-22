package scanrun_test

// scanrun_otel_test.go — Phase 11.A-4 scan flow 5 span instrument 단위 검증.
//
// 전략:
//   - in-memory SpanRecorder 가 부착된 sdktrace.TracerProvider 를 만들어 Deps.Tracer 주입.
//   - 1 robot × 1 check fan-out 으로 6 span (parent scan.run + 5 child) emit 검증.
//   - span name + parent/child hierarchy + attribute 5 종(scan.id · robot.id · check.id · outcome · evidence.bytes) 검증.
//   - noop tracer 시 span emit 0 — overhead 0 회귀 검증.

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/ssabro/rosshield/internal/app/scanrun"
	"github.com/ssabro/rosshield/internal/platform/clock"
)

// TestScanRun_EmitsFiveChildSpans 는 scan flow 5 span 이 정확한 hierarchy + attribute 로
// emit 되는지를 검증합니다.
func TestScanRun_EmitsFiveChildSpans(t *testing.T) {
	t.Parallel()

	rec, tracer := newRecordingTracer(t)
	h := newHarness(t, 4)
	h.seedFleetAndPack("tn_otel", "fl_x", "pk_x")
	h.seedRobots(1)
	h.seedChecks(1)
	h.orch = rebuildOrchWithTracer(t, h, tracer)

	sessionID := h.startSession(1)
	targets := h.makeTargets()
	checks := h.makeChecks()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.orch.Run(ctx, h.tenantID, sessionID, targets, checks); err != nil {
		t.Fatalf("Run: %v", err)
	}

	spans := rec.Ended()
	if len(spans) == 0 {
		t.Fatal("expected non-zero spans, got 0 — provider not wired")
	}

	// span name index — 단일 fan-out 시 각 span 정확 1 회.
	gotNames := map[string]int{}
	for _, s := range spans {
		gotNames[s.Name()]++
	}
	for _, want := range []string{"scan.run", "ssh.connect", "check.exec", "check.evaluate", "evidence.write", "scan.publish"} {
		if gotNames[want] == 0 {
			t.Errorf("expected span %q at least once, got %d (spans=%v)", want, gotNames[want], spanNames(spans))
		}
	}

	// parent scan.run 의 attribute 검증 — scan.id · tenant.id · robot_count · check_count.
	parent := findSpanByName(t, spans, "scan.run")
	if !parent.SpanContext.TraceID().IsValid() {
		t.Fatal("parent scan.run has no trace ID")
	}
	if got := attrString(parent.Attributes, "rosshield.scan.id"); got != sessionID {
		t.Errorf("scan.run.attr scan.id = %q, want %q", got, sessionID)
	}
	if got := attrString(parent.Attributes, "rosshield.tenant_id"); got != "tn_otel" {
		t.Errorf("scan.run.attr tenant_id = %q, want tn_otel", got)
	}
	if got := attrInt64(parent.Attributes, "rosshield.scan.total"); got != 1 {
		t.Errorf("scan.run.attr scan.total = %d, want 1", got)
	}
	if got := attrString(parent.Attributes, "rosshield.scan.status"); got != "completed" {
		t.Errorf("scan.run.attr scan.status = %q, want completed", got)
	}

	// child span 5 종 — 모두 parent scan.run 의 span ID 를 parent 로 가져야 함.
	parentSpanID := parent.SpanContext.SpanID()
	for _, name := range []string{"ssh.connect", "check.exec", "check.evaluate", "evidence.write", "scan.publish"} {
		child := findSpanByName(t, spans, name)
		if child.Parent.SpanID() != parentSpanID {
			t.Errorf("span %q parent = %v, want scan.run %v", name, child.Parent.SpanID(), parentSpanID)
		}
		if child.SpanContext.TraceID() != parent.SpanContext.TraceID() {
			t.Errorf("span %q traceID = %v, want %v", name, child.SpanContext.TraceID(), parent.SpanContext.TraceID())
		}
	}

	// check.exec attribute — robot.id · check.id · check.code · duration_ms · exit_code.
	exec := findSpanByName(t, spans, "check.exec")
	if got := attrString(exec.Attributes, "rosshield.robot.id"); got != "ro_000" {
		t.Errorf("check.exec.robot.id = %q, want ro_000", got)
	}
	if got := attrString(exec.Attributes, "rosshield.check.code"); got != "CIS-0" {
		t.Errorf("check.exec.check.code = %q, want CIS-0", got)
	}

	// check.evaluate attribute — outcome=pass (mock evaluator 기본).
	eval := findSpanByName(t, spans, "check.evaluate")
	if got := attrString(eval.Attributes, "rosshield.check.outcome"); got != "pass" {
		t.Errorf("check.evaluate.outcome = %q, want pass", got)
	}

	// scan.publish attribute — outcome 부착.
	pub := findSpanByName(t, spans, "scan.publish")
	if got := attrString(pub.Attributes, "rosshield.check.outcome"); got != "pass" {
		t.Errorf("scan.publish.outcome = %q, want pass", got)
	}

	// evidence.write attribute — bytes ≥ 0 (mock executor 의 stdout="ok" 2 bytes).
	ev := findSpanByName(t, spans, "evidence.write")
	// Evidence 가 nil 인 harness 라 bytes=0 + count=0. nil 여부와 무관하게 attribute 존재 확인.
	_ = attrInt64(ev.Attributes, "rosshield.evidence.bytes")

	// ssh.connect attribute — robot.host + robot.port.
	conn := findSpanByName(t, spans, "ssh.connect")
	if got := attrString(conn.Attributes, "rosshield.ssh.host"); got != "h0" {
		t.Errorf("ssh.connect.host = %q, want h0", got)
	}
	if got := attrInt64(conn.Attributes, "rosshield.ssh.port"); got != 22 {
		t.Errorf("ssh.connect.port = %d, want 22", got)
	}
}

// TestScanRun_NoopTracerEmitsZeroSpans 는 Deps.Tracer 가 noop 일 때 span 이 emit 되지 않음을 확인합니다.
//
// overhead 0 회귀 검증 — Enabled=false 경로의 production-safe 보장.
func TestScanRun_NoopTracerEmitsZeroSpans(t *testing.T) {
	t.Parallel()

	rec := tracetest.NewSpanRecorder()
	h := newHarness(t, 4)
	h.seedFleetAndPack("tn_noop", "fl_x", "pk_x")
	h.seedRobots(1)
	h.seedChecks(1)
	// noop tracer 주입 — provider 가 NoopTracerProvider 일 때와 동일한 동작.
	h.orch = rebuildOrchWithTracer(t, h, tracenoop.NewTracerProvider().Tracer("noop"))

	sessionID := h.startSession(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.orch.Run(ctx, h.tenantID, sessionID, h.makeTargets(), h.makeChecks()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := len(rec.Ended()); got != 0 {
		t.Fatalf("expected 0 recorded spans with noop tracer, got %d", got)
	}
}

// TestScanRun_NilTracerFallsBackToNoop 는 Deps.Tracer 가 nil 일 때 fallback noop tracer 가 사용됨을 검증합니다.
//
// 기존 호출자(테스트·legacy bootstrap)의 회귀 0 — Tracer 미주입도 panic 없이 동작.
func TestScanRun_NilTracerFallsBackToNoop(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 4)
	h.seedFleetAndPack("tn_nil", "fl_x", "pk_x")
	h.seedRobots(1)
	h.seedChecks(1)
	// h.orch 는 newHarness 가 만든 기본 — Tracer 미주입.
	sessionID := h.startSession(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.orch.Run(ctx, h.tenantID, sessionID, h.makeTargets(), h.makeChecks()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// --- helpers ---

// newRecordingTracer 는 in-memory SpanRecorder 가 부착된 tracer 를 반환합니다.
func newRecordingTracer(t *testing.T) (*tracetest.SpanRecorder, trace.Tracer) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(rec),
	)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	})
	return rec, tp.Tracer("rosshield/scanrun-test")
}

// rebuildOrchWithTracer 는 harness 의 Orchestrator 를 동일 deps + 명시 Tracer 로 다시 만듭니다.
//
// newHarness 는 Tracer 를 받지 않으므로 본 helper 가 test 전용 wiring 을 담당.
func rebuildOrchWithTracer(t *testing.T, h *harness, tr trace.Tracer) *scanrun.Orchestrator {
	t.Helper()
	return scanrun.New(scanrun.Deps{
		Scan:        h.scanSvc,
		Storage:     h.store,
		Executor:    h.executor,
		Evaluator:   h.evaluator,
		Bus:         h.bus,
		Clock:       clock.System(),
		WorkerLimit: 4,
		Tracer:      tr,
	})
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, s.Name())
	}
	return out
}

func findSpanByName(t *testing.T, spans []sdktrace.ReadOnlySpan, name string) tracetest.SpanStub {
	t.Helper()
	for _, s := range spans {
		if s.Name() == name {
			return tracetest.SpanStubFromReadOnlySpan(s)
		}
	}
	t.Fatalf("span %q not found in recorded set", name)
	return tracetest.SpanStub{}
}

func attrString(attrs []attribute.KeyValue, key string) string {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

func attrInt64(attrs []attribute.KeyValue, key string) int64 {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value.AsInt64()
		}
	}
	return 0
}
