package metrics

// eventbridge.go — EventBus 도메인 이벤트를 metric counter로 변환합니다.
//
// P5 도메인 결합 회피: 도메인 service는 metric을 직접 호출하지 않고, 본 bridge가 EventBus
// 구독으로 후처리. webhookrun.EventBridge와 같은 패턴.
//
// 구독 topic:
//
//	scan.started        → ScansStartedTotal{tenant}.Inc()
//	scan.completed      → ScansCompletedTotal{tenant, status}.Inc() + ScanFailedChecksTotal{tenant}.Add(failed)
//	invitation.sent     → InvitationsSentTotal{tenant}.Inc()
//	invitation.accepted → InvitationsAcceptedTotal{tenant}.Inc()
//	audit.checkpoint    → AuditChainHeadSeq{tenant}.Set(seq) — payload 파싱
//
// webhook delivery counter는 dispatcher에서 직접 증가하는 게 더 정확 — 본 bridge에서는 처리 안 함.

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/ssabro/rosshield/internal/platform/eventbus"
)

// MetricsBridge는 EventBus → Registry counter 변환의 결선 글루입니다.
type MetricsBridge struct {
	logger *slog.Logger
	reg    *Registry
	subs   []eventbus.Subscription
}

// NewBridge는 새 MetricsBridge를 만듭니다.
func NewBridge(logger *slog.Logger, reg *Registry) *MetricsBridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &MetricsBridge{logger: logger, reg: reg}
}

// Start는 모든 토픽에 EventBus 구독을 시작합니다 (idempotent).
//
// 호출 시점: bootstrap 결선 후. 두 번째 호출은 무시.
func (b *MetricsBridge) Start(ctx context.Context, bus eventbus.Bus) {
	if len(b.subs) > 0 {
		return
	}

	subscriptions := []struct {
		topic   string
		handler eventbus.Handler
	}{
		{"scan.started", b.handleScanStarted},
		{"scan.completed", b.handleScanCompleted},
		{"invitation.sent", b.handleInvitationSent},
		{"invitation.accepted", b.handleInvitationAccepted},
		{"audit.checkpoint", b.handleAuditCheckpoint},
	}

	for _, s := range subscriptions {
		s := s
		sub := bus.Subscribe(ctx, s.topic, s.handler)
		b.subs = append(b.subs, sub)
	}
	b.logger.Info("metrics event bridge started",
		"topics", []string{"scan.started", "scan.completed", "invitation.sent", "invitation.accepted", "audit.checkpoint"})
}

// Stop은 모든 구독을 cancel하고 worker 종료를 대기합니다.
func (b *MetricsBridge) Stop() {
	for _, sub := range b.subs {
		sub.Cancel()
	}
	for _, sub := range b.subs {
		<-sub.Done()
	}
	b.subs = nil
}

// === handlers ===

func (b *MetricsBridge) handleScanStarted(_ context.Context, evt eventbus.Event) error {
	if evt.TenantID == "" {
		return nil
	}
	b.reg.ScansStartedTotal.WithLabelValues(evt.TenantID).Inc()
	return nil
}

// handleScanCompleted는 scan.completed 이벤트에서 status·failed count를 추출 → 두 metric 갱신.
//
// payload schema (scan.CompletedEventPayload): {"sessionId":"...","status":"completed|failed|cancelled",
// "reason":"...","total":N,"completed":N,"failed":N}.
//
// usage 통계 (sales pitch / onboarding billing 자료):
//   - ScansCompletedTotal{tenant, status} — completed/failed/cancelled 분포
//   - ScanFailedChecksTotal{tenant} — 누적 violation 카운트 (compliance KPI)
func (b *MetricsBridge) handleScanCompleted(_ context.Context, evt eventbus.Event) error {
	if evt.TenantID == "" {
		return nil
	}
	var payload struct {
		Status string `json:"status"`
		Failed int64  `json:"failed"`
	}
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return nil
	}
	if payload.Status == "" {
		payload.Status = "unknown"
	}
	b.reg.ScansCompletedTotal.WithLabelValues(evt.TenantID, payload.Status).Inc()
	if payload.Failed > 0 {
		b.reg.ScanFailedChecksTotal.WithLabelValues(evt.TenantID).Add(float64(payload.Failed))
	}
	return nil
}

func (b *MetricsBridge) handleInvitationSent(_ context.Context, evt eventbus.Event) error {
	if evt.TenantID == "" {
		return nil
	}
	b.reg.InvitationsSentTotal.WithLabelValues(evt.TenantID).Inc()
	return nil
}

func (b *MetricsBridge) handleInvitationAccepted(_ context.Context, evt eventbus.Event) error {
	if evt.TenantID == "" {
		return nil
	}
	b.reg.InvitationsAcceptedTotal.WithLabelValues(evt.TenantID).Inc()
	return nil
}

// handleAuditCheckpoint는 payload에서 seq 추출 → AuditChainHeadSeq gauge 갱신.
//
// payload schema: {"seq": <int64>, "hash": "<hex>"}.
func (b *MetricsBridge) handleAuditCheckpoint(_ context.Context, evt eventbus.Event) error {
	if evt.TenantID == "" {
		return nil
	}
	var payload struct {
		Seq int64 `json:"seq"`
	}
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		// payload가 다르더라도 metric만 영향 — 무시.
		return nil
	}
	b.reg.AuditChainHeadSeq.WithLabelValues(evt.TenantID).Set(float64(payload.Seq))
	return nil
}

// IncWebhookDelivery는 dispatcher가 직접 호출하는 helper입니다.
//
// status: "success"|"failed"|"dead". metric reg를 dispatcher에 직접 노출하지 않고
// 본 메서드만 노출 — coupling 최소화.
func (r *Registry) IncWebhookDelivery(status string) {
	r.WebhookDeliveriesTotal.WithLabelValues(status).Inc()
}

// === HA leader-election metric helpers (E25 Stage 4 잔여) ===

// OnHAPromoted는 ha.Manager OnLeaderAcquired callback에서 호출됩니다.
//
// HARole=1, HALeaderEpoch=epoch, HAFailoverTotal+1.
// 본 helper는 race-safe — Prometheus client_golang의 atomic 업데이트.
func (r *Registry) OnHAPromoted(epoch int64) {
	r.HARole.Set(1)
	r.HALeaderEpoch.Set(float64(epoch))
	r.HAFailoverTotal.Inc()
}

// OnHADemoted는 ha.Manager OnLeaderLost callback에서 호출됩니다.
//
// HARole=0, HALeaderEpoch=0. HAFailoverTotal는 promotion에서만 증가 — demotion은 미증가.
func (r *Registry) OnHADemoted() {
	r.HARole.Set(0)
	r.HALeaderEpoch.Set(0)
}
