package metrics_test

// metrics_test.go — E27 Prometheus Registry + EventBridge 단위 테스트.
//
// 시나리오:
//
//	T1 TestMetricsEndpointExposesAllExpectedSeries — Handler() 응답에 핵심 시리즈 노출
//	T2 TestScanStartedMetricIncrementsOnce — scan.started 1회 publish → counter +1
//	T3 TestStructuredLogContainsTenantAndCorrelation — (eventbridge에서 검증 어려움 — 다른 stage)
//	T4 TestAuditCheckpointGaugeReflectsSeq — payload seq → gauge 값 갱신
//	T5 TestIncWebhookDeliveryByStatus — 직접 helper 호출 → counter +1

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/metrics"
)

func TestMetricsEndpointExposesAllExpectedSeries(t *testing.T) {
	t.Parallel()
	reg := metrics.New()

	// counter 1회 trigger — 빈 registry는 일부 시리즈가 안 나타남 (Prometheus 표준).
	reg.ScansStartedTotal.WithLabelValues("tn_T").Inc()
	reg.ScansCompletedTotal.WithLabelValues("tn_T", "completed").Inc()
	reg.ScanFailedChecksTotal.WithLabelValues("tn_T").Add(3)
	reg.WebhookDeliveriesTotal.WithLabelValues("success").Inc()
	reg.InvitationsSentTotal.WithLabelValues("tn_T").Inc()
	reg.InvitationsAcceptedTotal.WithLabelValues("tn_T").Inc()
	reg.AuditChainHeadSeq.WithLabelValues("tn_T").Set(42)
	reg.EventPublishDuration.WithLabelValues("scan.started").Observe(0.005)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	reg.Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"rosshield_scan_started_total",
		"rosshield_scan_completed_total",
		"rosshield_scan_failed_checks_total",
		"rosshield_webhook_deliveries_total",
		"rosshield_invitation_sent_total",
		"rosshield_invitation_accepted_total",
		"rosshield_audit_chain_head_seq",
		"rosshield_event_publish_duration_seconds",
		// E25 Stage 4 잔여 — HA leader-election metrics (gauge·counter는 빈 registry에서도 노출)
		"rosshield_ha_role",
		"rosshield_ha_leader_epoch",
		"rosshield_ha_failover_total",
		// process + go runtime collector
		"go_goroutines",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics missing series %q", want)
		}
	}
}

func TestScanStartedMetricIncrementsOnce(t *testing.T) {
	t.Parallel()
	reg := metrics.New()
	bridge, bus := newBridgeFixture(t, reg)
	defer cleanupBridgeFixture(t, bridge, bus)

	if err := bus.Publish(context.Background(), eventbus.Event{
		Type: "scan.started", Version: 1, TenantID: "tn_X",
		Payload: []byte(`{}`),
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	waitForCounterValue(t, reg, "rosshield_scan_started_total", 1, 1*time.Second)
}

func TestScanCompletedMetricRecordsStatusAndFailedChecks(t *testing.T) {
	t.Parallel()
	reg := metrics.New()
	bridge, bus := newBridgeFixture(t, reg)
	defer cleanupBridgeFixture(t, bridge, bus)

	// completed scan with 5 failed checks
	if err := bus.Publish(context.Background(), eventbus.Event{
		Type: "scan.completed", Version: 1, TenantID: "tn_X",
		Payload: []byte(`{"sessionId":"s1","status":"completed","failed":5}`),
	}); err != nil {
		t.Fatalf("Publish completed: %v", err)
	}
	// failed scan with 0 failed checks (terminal failure 자체)
	if err := bus.Publish(context.Background(), eventbus.Event{
		Type: "scan.completed", Version: 1, TenantID: "tn_X",
		Payload: []byte(`{"sessionId":"s2","status":"failed","failed":0}`),
	}); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	waitForCounterValue(t, reg, `rosshield_scan_completed_total{status="completed",tenant="tn_X"}`, 1, 1*time.Second)
	waitForCounterValue(t, reg, `rosshield_scan_completed_total{status="failed",tenant="tn_X"}`, 1, 1*time.Second)
	waitForCounterValue(t, reg, "rosshield_scan_failed_checks_total", 5, 1*time.Second)
}

func TestAuditCheckpointGaugeReflectsSeq(t *testing.T) {
	t.Parallel()
	reg := metrics.New()
	bridge, bus := newBridgeFixture(t, reg)
	defer cleanupBridgeFixture(t, bridge, bus)

	if err := bus.Publish(context.Background(), eventbus.Event{
		Type: "audit.checkpoint", Version: 1, TenantID: "tn_X",
		Payload: []byte(`{"seq":123,"hash":"deadbeef"}`),
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	waitForGaugeValue(t, reg, "rosshield_audit_chain_head_seq", 123, 1*time.Second)
}

func TestIncWebhookDeliveryByStatus(t *testing.T) {
	t.Parallel()
	reg := metrics.New()
	reg.IncWebhookDelivery("success")
	reg.IncWebhookDelivery("failed")
	reg.IncWebhookDelivery("failed")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	reg.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()

	if !strings.Contains(body, `rosshield_webhook_deliveries_total{status="success"} 1`) {
		t.Errorf("missing success=1 in body")
	}
	if !strings.Contains(body, `rosshield_webhook_deliveries_total{status="failed"} 2`) {
		t.Errorf("missing failed=2 in body")
	}
}

// E25 Stage 4 잔여 — HA 메트릭 promote/demote callback 검증.
func TestHAPromoteAndDemoteUpdatesMetrics(t *testing.T) {
	t.Parallel()
	reg := metrics.New()

	// 부팅 직후 — 모두 0.
	scrape := func() string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/metrics", nil)
		reg.Handler().ServeHTTP(rec, req)
		return rec.Body.String()
	}
	body := scrape()
	if !strings.Contains(body, "rosshield_ha_role 0") {
		t.Errorf("initial body missing 'rosshield_ha_role 0': %s", body)
	}
	if !strings.Contains(body, "rosshield_ha_failover_total 0") {
		t.Errorf("initial body missing 'rosshield_ha_failover_total 0'")
	}

	// 첫 promote — role=1, epoch=42, failover_total=1.
	reg.OnHAPromoted(42)
	body = scrape()
	if !strings.Contains(body, "rosshield_ha_role 1") {
		t.Errorf("after promote: body missing 'rosshield_ha_role 1'")
	}
	if !strings.Contains(body, "rosshield_ha_leader_epoch 42") {
		t.Errorf("after promote: body missing 'rosshield_ha_leader_epoch 42'")
	}
	if !strings.Contains(body, "rosshield_ha_failover_total 1") {
		t.Errorf("after promote: body missing 'rosshield_ha_failover_total 1'")
	}

	// demote — role=0, epoch=0, failover_total은 그대로 1 (demote는 미증가).
	reg.OnHADemoted()
	body = scrape()
	if !strings.Contains(body, "rosshield_ha_role 0") {
		t.Errorf("after demote: body missing 'rosshield_ha_role 0'")
	}
	if !strings.Contains(body, "rosshield_ha_leader_epoch 0") {
		t.Errorf("after demote: body missing 'rosshield_ha_leader_epoch 0'")
	}
	if !strings.Contains(body, "rosshield_ha_failover_total 1") {
		t.Errorf("after demote: failover_total should remain 1 (demote no-op for counter)")
	}

	// 두 번째 promote — failover_total=2, epoch=43.
	reg.OnHAPromoted(43)
	body = scrape()
	if !strings.Contains(body, "rosshield_ha_failover_total 2") {
		t.Errorf("after re-promote: body missing 'rosshield_ha_failover_total 2'")
	}
	if !strings.Contains(body, "rosshield_ha_leader_epoch 43") {
		t.Errorf("after re-promote: body missing 'rosshield_ha_leader_epoch 43'")
	}
}

// === fixture helpers ===

func newBridgeFixture(t *testing.T, reg *metrics.Registry) (*metrics.MetricsBridge, *inproc.Bus) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	bus := inproc.New(inproc.Deps{Logger: logger, Clock: clock.System(), IDGen: idgen.NewULID()})
	bridge := metrics.NewBridge(logger, reg)
	bridge.Start(context.Background(), bus)
	return bridge, bus
}

func cleanupBridgeFixture(t *testing.T, bridge *metrics.MetricsBridge, bus *inproc.Bus) {
	t.Helper()
	bridge.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = bus.Close(ctx)
}

func waitForCounterValue(t *testing.T, reg *metrics.Registry, seriesName string, want float64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if scrapeValue(reg, seriesName) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("counter %q did not reach %v within %v (current=%v)", seriesName, want, timeout, scrapeValue(reg, seriesName))
}

func waitForGaugeValue(t *testing.T, reg *metrics.Registry, seriesName string, want float64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if scrapeValue(reg, seriesName) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("gauge %q != %v within %v (current=%v)", seriesName, want, timeout, scrapeValue(reg, seriesName))
}

// scrapeValue는 /metrics 응답에서 첫 series 라인의 값을 파싱합니다 (단순 substring 기반).
//
// 정확한 label 일치 검사가 필요하면 별도 helper. 본 테스트는 단일 label set이라 충분.
func scrapeValue(reg *metrics.Registry, seriesName string) float64 {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	reg.Handler().ServeHTTP(rec, req)
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, seriesName) && !strings.HasPrefix(line, "# ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				v, err := strconv.ParseFloat(parts[len(parts)-1], 64)
				if err != nil {
					return 0
				}
				return v
			}
		}
	}
	return 0
}
