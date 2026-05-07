package sqliterepo_test

// repo_test.go — E23 webhook sqliterepo 단위 테스트.
//
// CRUD + tenant scope 격리 + Enqueue 필터링을 검증합니다.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/integration/webhook"
	"github.com/ssabro/rosshield/internal/domain/integration/webhook/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

// === harness ===

const (
	testTenant      = "tn_E23A"
	testTenantOther = "tn_E23B"
)

func newTestRepo(t *testing.T) (*sqliterepo.Repo, storage.Storage) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: filepath.Join(dir, "webhook.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		for _, tid := range []string{testTenant, testTenantOther} {
			if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'wh-test', 'desktop_free', ?)`,
				tid, now); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}

	repo := sqliterepo.New(sqliterepo.Deps{
		Clock: clock.System(),
		IDGen: idgen.NewULID(),
	})
	return repo, store
}

func tenantCtx(tid string) context.Context {
	return storage.WithTenantID(context.Background(), storage.TenantID(tid))
}

func sampleEndpoint() webhook.WebhookEndpoint {
	return webhook.WebhookEndpoint{
		URL:     "https://siem.example.com/in",
		Secret:  "shared-key",
		Events:  []webhook.EventType{webhook.EventScanCompleted, webhook.EventInsightCreated},
		Format:  webhook.PayloadFormatJSON,
		Enabled: true,
	}
}

// === tests ===

func TestCreateEndpointGeneratesIDAndPersists(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	var created webhook.WebhookEndpoint
	if err := store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		ep, err := repo.CreateEndpoint(ctx, tx, sampleEndpoint())
		created = ep
		return err
	}); err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	if created.ID == "" {
		t.Errorf("ID empty")
	}
	if created.TenantID != testTenant {
		t.Errorf("TenantID = %q, want %q", created.TenantID, testTenant)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Errorf("timestamps not set")
	}

	// Get으로 조회 — 같은 데이터.
	var fetched webhook.WebhookEndpoint
	_ = store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		ep, err := repo.GetEndpoint(ctx, tx, created.ID)
		fetched = ep
		return err
	})
	if fetched.URL != created.URL || fetched.Secret != created.Secret {
		t.Errorf("Get mismatch: %+v vs %+v", fetched, created)
	}
	if len(fetched.Events) != 2 {
		t.Errorf("len events = %d, want 2", len(fetched.Events))
	}
}

func TestCreateEndpointRejectsInvalidURL(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	ep := sampleEndpoint()
	ep.URL = "not-a-url"
	err := store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.CreateEndpoint(ctx, tx, ep)
		return e
	})
	if !errors.Is(err, webhook.ErrInvalidURL) {
		t.Errorf("err = %v, want ErrInvalidURL", err)
	}
}

func TestCreateEndpointRejectsEmptySecret(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	ep := sampleEndpoint()
	ep.Secret = ""
	err := store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.CreateEndpoint(ctx, tx, ep)
		return e
	})
	if !errors.Is(err, webhook.ErrEmptySecret) {
		t.Errorf("err = %v, want ErrEmptySecret", err)
	}
}

func TestCreateEndpointRejectsUnknownEvent(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	ep := sampleEndpoint()
	ep.Events = []webhook.EventType{"unknown.kind"}
	err := store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.CreateEndpoint(ctx, tx, ep)
		return e
	})
	if !errors.Is(err, webhook.ErrInvalidEvent) {
		t.Errorf("err = %v, want ErrInvalidEvent", err)
	}
}

func TestUpdateEndpointModifiesFields(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	var created webhook.WebhookEndpoint
	_ = store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		ep, _ := repo.CreateEndpoint(ctx, tx, sampleEndpoint())
		created = ep
		return nil
	})

	created.URL = "https://new.example.com/in"
	created.Enabled = false
	created.Events = []webhook.EventType{webhook.EventAuditCheckpoint}
	if err := store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.UpdateEndpoint(ctx, tx, created)
		return e
	}); err != nil {
		t.Fatalf("UpdateEndpoint: %v", err)
	}

	var fetched webhook.WebhookEndpoint
	_ = store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		ep, _ := repo.GetEndpoint(ctx, tx, created.ID)
		fetched = ep
		return nil
	})
	if fetched.URL != "https://new.example.com/in" {
		t.Errorf("URL = %q, want updated", fetched.URL)
	}
	if fetched.Enabled {
		t.Errorf("Enabled = true, want false")
	}
	if len(fetched.Events) != 1 || fetched.Events[0] != webhook.EventAuditCheckpoint {
		t.Errorf("Events = %v, want [audit.checkpoint]", fetched.Events)
	}
}

func TestUpdateEndpointMissingReturnsNotFound(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	ep := sampleEndpoint()
	ep.ID = "wh_doesnotexist"
	err := store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.UpdateEndpoint(ctx, tx, ep)
		return e
	})
	if !errors.Is(err, webhook.ErrEndpointNotFound) {
		t.Errorf("err = %v, want ErrEndpointNotFound", err)
	}
}

func TestDeleteEndpointRemovesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	var created webhook.WebhookEndpoint
	_ = store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		ep, _ := repo.CreateEndpoint(ctx, tx, sampleEndpoint())
		created = ep
		return nil
	})

	if err := store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		return repo.DeleteEndpoint(ctx, tx, created.ID)
	}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// 두 번째 삭제 → ErrEndpointNotFound.
	err := store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		return repo.DeleteEndpoint(ctx, tx, created.ID)
	})
	if !errors.Is(err, webhook.ErrEndpointNotFound) {
		t.Errorf("second delete err = %v, want ErrEndpointNotFound", err)
	}
}

// 핵심 P4 — cross-tenant 조회는 ErrEndpointNotFound.
func TestGetEndpointCrossTenantIsolated(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	var created webhook.WebhookEndpoint
	_ = store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		ep, _ := repo.CreateEndpoint(ctx, tx, sampleEndpoint())
		created = ep
		return nil
	})

	err := store.Tx(tenantCtx(testTenantOther), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.GetEndpoint(ctx, tx, created.ID)
		return e
	})
	if !errors.Is(err, webhook.ErrEndpointNotFound) {
		t.Errorf("cross-tenant Get err = %v, want ErrEndpointNotFound", err)
	}

	// ListEndpoints from other tenant은 빈 결과.
	var others []webhook.WebhookEndpoint
	_ = store.Tx(tenantCtx(testTenantOther), func(ctx context.Context, tx storage.Tx) error {
		out, _ := repo.ListEndpoints(ctx, tx)
		others = out
		return nil
	})
	if len(others) != 0 {
		t.Errorf("other tenant ListEndpoints = %d, want 0", len(others))
	}
}

func TestListEndpointsReturnsTenantScoped(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	// tenant A에 2개, tenant B에 1개.
	for i := 0; i < 2; i++ {
		_ = store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
			_, _ = repo.CreateEndpoint(ctx, tx, sampleEndpoint())
			return nil
		})
		time.Sleep(2 * time.Millisecond)
	}
	_ = store.Tx(tenantCtx(testTenantOther), func(ctx context.Context, tx storage.Tx) error {
		_, _ = repo.CreateEndpoint(ctx, tx, sampleEndpoint())
		return nil
	})

	var tenantA []webhook.WebhookEndpoint
	_ = store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		out, _ := repo.ListEndpoints(ctx, tx)
		tenantA = out
		return nil
	})
	if len(tenantA) != 2 {
		t.Errorf("tenant A list = %d, want 2", len(tenantA))
	}
}

// Enqueue: enabled=true + Events 필터 통과인 endpoint만 delivery 생성.
func TestEnqueueRespectsEnabledAndEventFilter(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	// 3 endpoints:
	//   ep1: enabled, scan.completed만 구독 → match.
	//   ep2: enabled, audit.checkpoint만 구독 → no match.
	//   ep3: disabled, scan.completed 구독 → no match (disabled).
	var ep1, ep2, ep3 webhook.WebhookEndpoint
	_ = store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		e1 := sampleEndpoint()
		e1.Events = []webhook.EventType{webhook.EventScanCompleted}
		ep1, _ = repo.CreateEndpoint(ctx, tx, e1)

		e2 := sampleEndpoint()
		e2.Events = []webhook.EventType{webhook.EventAuditCheckpoint}
		ep2, _ = repo.CreateEndpoint(ctx, tx, e2)

		e3 := sampleEndpoint()
		e3.Events = []webhook.EventType{webhook.EventScanCompleted}
		e3.Enabled = false
		ep3, _ = repo.CreateEndpoint(ctx, tx, e3)
		return nil
	})

	payload, _ := json.Marshal(map[string]string{"scan_id": "ss_X", "outcome": "pass"})
	evt := webhook.DomainEvent{
		EventID:    "evt_01H8X",
		TenantID:   testTenant,
		Type:       webhook.EventScanCompleted,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	}

	var deliveries []webhook.WebhookDelivery
	if err := store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		out, e := repo.Enqueue(ctx, tx, evt)
		deliveries = out
		return e
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if len(deliveries) != 1 {
		t.Fatalf("len deliveries = %d, want 1 (only ep1 matches)", len(deliveries))
	}
	if deliveries[0].EndpointID != ep1.ID {
		t.Errorf("delivery for %q, want ep1=%q", deliveries[0].EndpointID, ep1.ID)
	}
	if deliveries[0].EventType != webhook.EventScanCompleted {
		t.Errorf("event_type = %q, want scan.completed", deliveries[0].EventType)
	}
	if deliveries[0].AttemptCount != 0 {
		t.Errorf("attempt_count = %d, want 0", deliveries[0].AttemptCount)
	}
	if string(deliveries[0].Payload) != string(payload) {
		t.Errorf("payload mismatch")
	}

	// ep2/ep3는 delivery 없음.
	for _, ep := range []webhook.WebhookEndpoint{ep2, ep3} {
		var ds []webhook.WebhookDelivery
		_ = store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
			out, _ := repo.ListDeliveries(ctx, tx, ep.ID, 0)
			ds = out
			return nil
		})
		if len(ds) != 0 {
			t.Errorf("endpoint %q deliveries = %d, want 0", ep.ID, len(ds))
		}
	}
}

func TestEnqueueEmptyEventsSubscribesToAll(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	// Events nil → 모든 known event 구독.
	var ep webhook.WebhookEndpoint
	_ = store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		e := sampleEndpoint()
		e.Events = nil
		ep, _ = repo.CreateEndpoint(ctx, tx, e)
		return nil
	})

	for _, evtType := range webhook.KnownEventTypes {
		evt := webhook.DomainEvent{
			EventID:    "evt_" + string(evtType),
			TenantID:   testTenant,
			Type:       evtType,
			OccurredAt: time.Now().UTC(),
		}
		var ds []webhook.WebhookDelivery
		if err := store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
			out, e := repo.Enqueue(ctx, tx, evt)
			ds = out
			return e
		}); err != nil {
			t.Fatalf("Enqueue %s: %v", evtType, err)
		}
		if len(ds) != 1 {
			t.Errorf("event %q: deliveries = %d, want 1", evtType, len(ds))
		}
	}

	// endpoint별 ListDeliveries 회수 — 3건 (각 event 1개씩).
	var all []webhook.WebhookDelivery
	_ = store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		out, _ := repo.ListDeliveries(ctx, tx, ep.ID, 0)
		all = out
		return nil
	})
	if len(all) != len(webhook.KnownEventTypes) {
		t.Errorf("total deliveries = %d, want %d", len(all), len(webhook.KnownEventTypes))
	}
}

func TestEnqueueRequiresTenantContext(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.Enqueue(ctx, tx, webhook.DomainEvent{Type: webhook.EventScanCompleted})
		return e
	})
	if !errors.Is(err, storage.ErrTenantMissing) {
		t.Errorf("err = %v, want ErrTenantMissing", err)
	}
}

func TestGetDeliveryCrossTenantIsolated(t *testing.T) {
	t.Parallel()
	repo, store := newTestRepo(t)

	var d webhook.WebhookDelivery
	_ = store.Tx(tenantCtx(testTenant), func(ctx context.Context, tx storage.Tx) error {
		_, _ = repo.CreateEndpoint(ctx, tx, sampleEndpoint())
		ds, _ := repo.Enqueue(ctx, tx, webhook.DomainEvent{
			EventID:    "evt_X",
			TenantID:   testTenant,
			Type:       webhook.EventScanCompleted,
			OccurredAt: time.Now().UTC(),
		})
		if len(ds) > 0 {
			d = ds[0]
		}
		return nil
	})
	if d.ID == "" {
		t.Fatalf("no delivery created")
	}

	err := store.Tx(tenantCtx(testTenantOther), func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.GetDelivery(ctx, tx, d.ID)
		return e
	})
	if !errors.Is(err, webhook.ErrDeliveryNotFound) {
		t.Errorf("cross-tenant Get err = %v, want ErrDeliveryNotFound", err)
	}
}
