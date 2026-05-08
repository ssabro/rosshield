package webhookrun_test

// eventbridge_test.go — E23-D EventBus → webhook.Enqueue bridge 통합 테스트.
//
// 시나리오:
//  1. scan.completed publish → tenant 매칭 endpoint에 delivery 1건 영속.
//  2. insight.created publish → 같은 tenant + insight 구독 endpoint에 delivery 1건.
//  3. audit.checkpoint publish → 같은 tenant + audit 구독 endpoint에 delivery 1건.
//  4. tenant 격리 — 다른 tenant 이벤트는 상대 tenant endpoint에 delivery 안 생김.
//  5. Stop 후 publish → delivery 안 생김 (구독 해제됐는지 확인).

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/app/webhookrun"
	"github.com/ssabro/rosshield/internal/domain/integration/webhook"
	webhookrepo "github.com/ssabro/rosshield/internal/domain/integration/webhook/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// bridgeFixture는 bridge 테스트용 결선 묶음입니다.
type bridgeFixture struct {
	store   storage.Storage
	bus     *inproc.Bus
	whSvc   webhook.Service
	bridge  *webhookrun.EventBridge
	closeFn func()
}

func newBridgeFixture(t *testing.T) *bridgeFixture {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}

	clk := clock.System()
	ids := idgen.NewULID()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	bus := inproc.New(inproc.Deps{Logger: logger, Clock: clk, IDGen: ids})

	whSvc := webhookrepo.New(webhookrepo.Deps{Clock: clk, IDGen: ids})

	bridge := webhookrun.NewBridge(webhookrun.BridgeDeps{
		Logger:  logger,
		Storage: store,
		Webhook: whSvc,
	})
	bridge.Start(context.Background(), bus)

	return &bridgeFixture{
		store:  store,
		bus:    bus,
		whSvc:  whSvc,
		bridge: bridge,
		closeFn: func() {
			bridge.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = bus.Close(ctx)
			_ = store.Close()
		},
	}
}

// seedTenant는 raw INSERT로 tenant + endpoint를 영속합니다.
//
// endpoint 등록은 webhook.Service.CreateEndpoint를 사용 (tenant scope tx 안에서).
func (f *bridgeFixture) seedTenantWithEndpoint(t *testing.T, tenantID string, events []webhook.EventType) string {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := f.store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'test', 'desktop_free', ?)`, tenantID, now)
		return err
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	tenantCtx := storage.WithTenantID(context.Background(), storage.TenantID(tenantID))
	var epID string
	if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		ep, e := f.whSvc.CreateEndpoint(ctx, tx, webhook.WebhookEndpoint{
			URL:     "https://siem.example.com/hook-" + tenantID,
			Secret:  "secret-1234",
			Events:  events,
			Format:  webhook.PayloadFormatJSON,
			Enabled: true,
		})
		if e != nil {
			return e
		}
		epID = ep.ID
		return nil
	}); err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	return epID
}

func (f *bridgeFixture) listDeliveries(t *testing.T, tenantID, endpointID string) []webhook.WebhookDelivery {
	t.Helper()
	tenantCtx := storage.WithTenantID(context.Background(), storage.TenantID(tenantID))
	var out []webhook.WebhookDelivery
	if err := f.store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		ds, e := f.whSvc.ListDeliveries(ctx, tx, endpointID, 100)
		out = ds
		return e
	}); err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	return out
}

// publishAndWait는 publish 후 bridge handler가 처리할 시간을 잠깐 대기합니다.
//
// EventBus는 async — handler 처리 완료를 보장하지 않으므로 짧은 polling.
func (f *bridgeFixture) publishAndWaitForCount(t *testing.T, tenantID, endpointID string, want int, timeout time.Duration) []webhook.WebhookDelivery {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var ds []webhook.WebhookDelivery
	for time.Now().Before(deadline) {
		ds = f.listDeliveries(t, tenantID, endpointID)
		if len(ds) >= want {
			return ds
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ds
}

// === 1. scan.completed publish → delivery 1건 ===

func TestBridgeScanCompletedEnqueues(t *testing.T) {
	f := newBridgeFixture(t)
	defer f.closeFn()

	tenantID := "tn_T1"
	epID := f.seedTenantWithEndpoint(t, tenantID, []webhook.EventType{webhook.EventScanCompleted})

	payload, _ := json.Marshal(map[string]any{"sessionId": "ss_X", "completed": 6, "failed": 0})
	if err := f.bus.Publish(context.Background(), eventbus.Event{
		Type:      "scan.completed",
		Version:   1,
		TenantID:  tenantID,
		Aggregate: eventbus.AggregateRef{Type: "ScanSession", ID: "ss_X"},
		Payload:   payload,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	ds := f.publishAndWaitForCount(t, tenantID, epID, 1, 1*time.Second)
	if len(ds) != 1 {
		t.Fatalf("deliveries=%d, want 1", len(ds))
	}
	if ds[0].EventType != webhook.EventScanCompleted {
		t.Errorf("event type = %q, want scan.completed", ds[0].EventType)
	}
	if ds[0].EndpointID != epID {
		t.Errorf("endpoint = %q, want %q", ds[0].EndpointID, epID)
	}
}

// === 2. insight.created publish → delivery 1건 ===

func TestBridgeInsightCreatedEnqueues(t *testing.T) {
	f := newBridgeFixture(t)
	defer f.closeFn()

	tenantID := "tn_T2"
	epID := f.seedTenantWithEndpoint(t, tenantID, []webhook.EventType{webhook.EventInsightCreated})

	payload, _ := json.Marshal(map[string]any{"insightId": "in_X", "severity": "high"})
	if err := f.bus.Publish(context.Background(), eventbus.Event{
		Type:      "insight.created",
		Version:   1,
		TenantID:  tenantID,
		Aggregate: eventbus.AggregateRef{Type: "Insight", ID: "in_X"},
		Payload:   payload,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	ds := f.publishAndWaitForCount(t, tenantID, epID, 1, 1*time.Second)
	if len(ds) != 1 {
		t.Fatalf("deliveries=%d, want 1", len(ds))
	}
	if ds[0].EventType != webhook.EventInsightCreated {
		t.Errorf("event type = %q, want insight.created", ds[0].EventType)
	}
}

// === 3. audit.checkpoint publish → delivery 1건 ===

func TestBridgeAuditCheckpointEnqueues(t *testing.T) {
	f := newBridgeFixture(t)
	defer f.closeFn()

	tenantID := "tn_T3"
	epID := f.seedTenantWithEndpoint(t, tenantID, []webhook.EventType{webhook.EventAuditCheckpoint})

	payload, _ := json.Marshal(map[string]any{"seq": 42, "hash": "abcd"})
	if err := f.bus.Publish(context.Background(), eventbus.Event{
		Type:      "audit.checkpoint",
		Version:   1,
		TenantID:  tenantID,
		Aggregate: eventbus.AggregateRef{Type: "AuditCheckpoint", ID: "42"},
		Payload:   payload,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	ds := f.publishAndWaitForCount(t, tenantID, epID, 1, 1*time.Second)
	if len(ds) != 1 {
		t.Fatalf("deliveries=%d, want 1", len(ds))
	}
	if ds[0].EventType != webhook.EventAuditCheckpoint {
		t.Errorf("event type = %q, want audit.checkpoint", ds[0].EventType)
	}
}

// === 4. tenant 격리 ===

func TestBridgeTenantIsolation(t *testing.T) {
	f := newBridgeFixture(t)
	defer f.closeFn()

	tenantA := "tn_A"
	tenantB := "tn_B"
	epA := f.seedTenantWithEndpoint(t, tenantA, []webhook.EventType{webhook.EventScanCompleted})
	epB := f.seedTenantWithEndpoint(t, tenantB, []webhook.EventType{webhook.EventScanCompleted})

	// publish for tenantA only.
	payload, _ := json.Marshal(map[string]any{"sessionId": "ss_only_A"})
	if err := f.bus.Publish(context.Background(), eventbus.Event{
		Type:      "scan.completed",
		Version:   1,
		TenantID:  tenantA,
		Aggregate: eventbus.AggregateRef{Type: "ScanSession", ID: "ss_only_A"},
		Payload:   payload,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	dsA := f.publishAndWaitForCount(t, tenantA, epA, 1, 1*time.Second)
	if len(dsA) != 1 {
		t.Fatalf("tenantA deliveries=%d, want 1", len(dsA))
	}
	dsB := f.listDeliveries(t, tenantB, epB)
	if len(dsB) != 0 {
		t.Errorf("tenantB deliveries=%d, want 0 (isolation breach)", len(dsB))
	}
}

// === 5. Stop 후 publish → delivery 안 생김 ===

func TestBridgeStopUnsubscribes(t *testing.T) {
	f := newBridgeFixture(t)

	tenantID := "tn_Stop"
	epID := f.seedTenantWithEndpoint(t, tenantID, []webhook.EventType{webhook.EventScanCompleted})

	f.bridge.Stop() // 본 테스트에선 일찍 Stop.
	// 이후 publish → bridge가 받지 않으므로 delivery 안 생김.

	payload, _ := json.Marshal(map[string]any{"sessionId": "ss_after_stop"})
	if err := f.bus.Publish(context.Background(), eventbus.Event{
		Type:      "scan.completed",
		Version:   1,
		TenantID:  tenantID,
		Aggregate: eventbus.AggregateRef{Type: "ScanSession", ID: "ss_after_stop"},
		Payload:   payload,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// 잠시 대기 후 검증 — bridge가 받았다면 처리됐을 시간.
	time.Sleep(100 * time.Millisecond)
	ds := f.listDeliveries(t, tenantID, epID)
	if len(ds) != 0 {
		t.Errorf("after Stop deliveries=%d, want 0", len(ds))
	}

	// closeFn에서 Stop 두 번 호출 — idempotent 검증.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = f.bus.Close(ctx)
	_ = f.store.Close()
}
