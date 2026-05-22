// scanrun_span.go — Phase 11.A-4 OpenTelemetry span helpers.
//
// 책임:
//   - scan flow 5 단계(ssh.connect · check.exec · check.evaluate · evidence.write
//     · scan.publish)에 child span 을 emit. parent 는 scan.run span (Run() 진입 시).
//   - Deps.Tracer 가 nil 이면 OpenTelemetry noop tracer 사용 — overhead 0 (R14-1 + R11A-2).
//   - PII 노출 회피 — ssh.host 는 IP/hostname 만 (credential 미노출). check 의
//     audit command argv 는 attribute 미부착(potential PII / secret 포함 가능).
//
// 도메인 경계:
//   - 본 helper 는 application layer(internal/app/scanrun/) — platform/otel 의존 가능.
//   - 도메인(internal/domain/scan/) 은 otel 직접 import 0 일관.
//
// 결정 항목 추적:
//   - D-P11A-1 = 옵션 A (otel SDK 전면) — 본 helper 가 scan flow hot path 의 span emit.
//   - D-P11A-3 = parent_based + ratio 0.05 — parent scan.run 의 sampling 결정을 child 5 단계 상속.

package scanrun

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// scanrunTracerScope 는 본 패키지가 emit 하는 span 의 instrumentation scope 입니다.
//
// bootstrap 이 deps.Tracer 를 명시 주입하지 않으면 fallback 으로 noop tracer 가 사용됩니다.
const scanrunTracerScope = "rosshield/scanrun"

// span name 상수 — design doc §6.4 명시.
const (
	spanScanRun        = "scan.run"
	spanSSHConnect     = "ssh.connect"
	spanCheckExec      = "check.exec"
	spanCheckEvaluate  = "check.evaluate"
	spanEvidenceWrite  = "evidence.write"
	spanScanPublish    = "scan.publish"
	spanScanRunPublish = "scan.publish.completed"
)

// attribute key 상수 — rosshield 커스텀 + OpenTelemetry semantic conventions.
//
// PII 회피 정책:
//   - ssh.host 는 robot.Host (IP 또는 hostname) 만 — credential 무관.
//   - ssh.user 는 attribute 미부착(audit log 와 cross-reference 위해 robot.id 만 충분).
//   - check.audit_command 미부착 — pack 별 argv 에 secret 포함 가능.
const (
	attrTenantID       = "rosshield.tenant_id"
	attrScanID         = "rosshield.scan.id"
	attrRobotID        = "rosshield.robot.id"
	attrRobotHost      = "rosshield.ssh.host"
	attrRobotPort      = "rosshield.ssh.port"
	attrCheckID        = "rosshield.check.id"
	attrCheckCode      = "rosshield.check.code"
	attrPackCheckID    = "rosshield.check.pack_check_id"
	attrCheckOutcome   = "rosshield.check.outcome"
	attrCheckReason    = "rosshield.check.reason"
	attrExecExitCode   = "rosshield.exec.exit_code"
	attrExecDurationMs = "rosshield.exec.duration_ms"
	attrEvidenceBytes  = "rosshield.evidence.bytes"
	attrEvidenceCount  = "rosshield.evidence.count"
	attrScanStatus     = "rosshield.scan.status"
	attrScanTotal      = "rosshield.scan.total"
	attrScanRobots     = "rosshield.scan.robot_count"
	attrScanChecks     = "rosshield.scan.check_count"
)

// tracerOrNoop 은 deps.Tracer 가 nil 이면 OpenTelemetry noop tracer 를 반환합니다.
//
// noop tracer 의 Start 는 사실상 비용 0 (no allocation, no export) — Enabled=false
// 경로의 overhead 0 보장. provider 의 sampler 가 NeverSample 일 때도 동일 효과.
func (o *Orchestrator) tracer() trace.Tracer {
	if o.deps.Tracer != nil {
		return o.deps.Tracer
	}
	return tracenoop.NewTracerProvider().Tracer(scanrunTracerScope)
}

// startScanRunSpan 은 Run() 진입 시 parent scan.run span 을 시작합니다.
//
// 반환 ctx 는 worker goroutine 들로 propagate — 5 child span 이 자동 nest.
// 호출자는 defer span.End() 책임 — finalize 직후 attribute 갱신 후 End.
func (o *Orchestrator) startScanRunSpan(ctx context.Context, sessionID, tenantID string, robotCount, checkCount int) (context.Context, trace.Span) {
	ctx, span := o.tracer().Start(ctx, spanScanRun,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String(attrScanID, sessionID),
			attribute.String(attrTenantID, tenantID),
			attribute.Int(attrScanRobots, robotCount),
			attribute.Int(attrScanChecks, checkCount),
			attribute.Int(attrScanTotal, robotCount*checkCount),
		),
	)
	return ctx, span
}

// startSSHConnectSpan 은 SSH dial 가시화 marker span 을 시작합니다.
//
// 실 dial 은 SSHExecutor.Exec 안의 sshpool.Pool 이 lazy 수행 — 본 span 은
// scan flow 시각으로 "ssh hop 을 시도했다" 표지. duration 은 dial+keepalive
// 합산이 아니라 span 시작~End 까지의 marker 윈도우(보통 short-lived).
func startSSHConnectSpan(ctx context.Context, tr trace.Tracer, target ssherTarget) (context.Context, trace.Span) {
	return tr.Start(ctx, spanSSHConnect,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String(attrRobotID, target.RobotID),
			attribute.String(attrRobotHost, target.Host),
			attribute.Int(attrRobotPort, target.Port),
		),
	)
}

// startCheckExecSpan 은 SSHExecutor.Exec 호출 전체를 감싸는 child span 을 시작합니다.
func startCheckExecSpan(ctx context.Context, tr trace.Tracer, robotID string, check checkInfo) (context.Context, trace.Span) {
	return tr.Start(ctx, spanCheckExec,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String(attrRobotID, robotID),
			attribute.String(attrCheckID, check.PackCheckID),
			attribute.String(attrCheckCode, check.Code),
			attribute.String(attrPackCheckID, check.PackCheckID),
		),
	)
}

// startCheckEvaluateSpan 은 CheckEvaluator.Evaluate 호출을 감싸는 child span 을 시작합니다.
func startCheckEvaluateSpan(ctx context.Context, tr trace.Tracer, robotID string, check checkInfo) (context.Context, trace.Span) {
	return tr.Start(ctx, spanCheckEvaluate,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String(attrRobotID, robotID),
			attribute.String(attrCheckID, check.PackCheckID),
			attribute.String(attrCheckCode, check.Code),
		),
	)
}

// startEvidenceWriteSpan 은 evidence.Service.Store + LinkToResult 호출을 감싸는 span 을 시작합니다.
func startEvidenceWriteSpan(ctx context.Context, tr trace.Tracer, robotID, checkID string) (context.Context, trace.Span) {
	return tr.Start(ctx, spanEvidenceWrite,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String(attrRobotID, robotID),
			attribute.String(attrCheckID, checkID),
		),
	)
}

// startScanPublishSpan 은 RecordResult + Bus.Publish("scan.progress") 호출을 감싸는 span 을 시작합니다.
func startScanPublishSpan(ctx context.Context, tr trace.Tracer, sessionID, robotID, checkID string) (context.Context, trace.Span) {
	return tr.Start(ctx, spanScanPublish,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String(attrScanID, sessionID),
			attribute.String(attrRobotID, robotID),
			attribute.String(attrCheckID, checkID),
		),
	)
}

// recordSpanErr 은 err 이 non-nil 일 때 span 에 error 를 기록합니다.
//
// nil err 은 no-op — defer 패턴으로 호출자가 단순화 가능.
func recordSpanErr(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// ssherTarget · checkInfo 는 span helper 가 도메인 struct 의 일부만 받기 위한 좁은 타입입니다.
//
// scan.RobotTarget · scan.CheckDef 의 모든 필드를 노출하지 않아 attribute 의 범위를
// 제한 + 단위 테스트 mock 단순화.
type ssherTarget struct {
	RobotID string
	Host    string
	Port    int
}

type checkInfo struct {
	PackCheckID string
	Code        string
}
