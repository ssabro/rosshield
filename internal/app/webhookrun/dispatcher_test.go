package webhookrun_test

// dispatcher_test.go — E23-B Process worker 단위 테스트.
//
// httptest.Server로 IdP를 모킹하고, 실제 sqliterepo + sqlite store 위에서 dispatcher를 돌려
// 다음 시나리오를 검증합니다:
//
//   - 200 응답 → succeeded=1 + last_response_status 갱신
//   - 500 응답 → attempt_count++ + next_attempt_at = now + 1m
//   - network 실패(invalid host) → attempt_count++ + last_error 채움
//   - HMAC 헤더(X-Rosshield-Signature) 검증 — 수신자가 본문으로 verify해서 일치 여부 카운트
//   - MaxRetryAttempts 도달 → 더 이상 dispatch 안 함 (ListDueDeliveries 결과에서 제외)
//   - 부팅 직후 즉시 dispatch (tick 대기 X)

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/app/webhookrun"
	"github.com/ssabro/rosshield/internal/domain/integration/webhook"
	"github.com/ssabro/rosshield/internal/domain/integration/webhook/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/idgen"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

const (
	testTenant = "tn_E23B"
	testSecret = "shared-secret"
)

// === harness ===

func newTestStorage(t *testing.T) (*sqliterepo.Repo, storage.Storage) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: filepath.Join(dir, "wh.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, plan, created_at) VALUES (?, 'wh-test', 'desktop_free', ?)`,
			testTenant, now)
		return e
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	repo := sqliterepo.New(sqliterepo.Deps{Clock: clock.System(), IDGen: idgen.NewULID()})
	return repo, store
}

// makeEndpointAndEnqueue는 endpoint를 만들고 1건 delivery를 enqueue한 뒤 (endpoint, delivery)를 반환합니다.
func makeEndpointAndEnqueue(t *testing.T, repo *sqliterepo.Repo, store storage.Storage, url string) (webhook.WebhookEndpoint, webhook.WebhookDelivery) {
	t.Helper()
	var ep webhook.WebhookEndpoint
	var del webhook.WebhookDelivery
	tenantCtx := storage.WithTenantID(context.Background(), storage.TenantID(testTenant))
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		out, err := repo.CreateEndpoint(ctx, tx, webhook.WebhookEndpoint{
			URL:     url,
			Secret:  testSecret,
			Events:  []webhook.EventType{webhook.EventScanCompleted},
			Format:  webhook.PayloadFormatJSON,
			Enabled: true,
		})
		if err != nil {
			return err
		}
		ep = out
		ds, err := repo.Enqueue(ctx, tx, webhook.DomainEvent{
			EventID:    "evt_E23B",
			TenantID:   testTenant,
			Type:       webhook.EventScanCompleted,
			OccurredAt: time.Now().UTC(),
			Payload:    []byte(`{"scan_id":"ss_X"}`),
		})
		if err != nil {
			return err
		}
		if len(ds) > 0 {
			del = ds[0]
		}
		return nil
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if del.ID == "" {
		t.Fatalf("no delivery enqueued")
	}
	return ep, del
}

// fetchDelivery는 delivery 1건의 현재 상태를 회수합니다.
func fetchDelivery(t *testing.T, repo *sqliterepo.Repo, store storage.Storage, deliveryID string) webhook.WebhookDelivery {
	t.Helper()
	var d webhook.WebhookDelivery
	tenantCtx := storage.WithTenantID(context.Background(), storage.TenantID(testTenant))
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		out, e := repo.GetDelivery(ctx, tx, deliveryID)
		d = out
		return e
	}); err != nil {
		t.Fatalf("GetDelivery: %v", err)
	}
	return d
}

// silentLogger는 log noise 제거용.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// === tests ===

// 200 응답이면 succeeded=1 + status·attempt 갱신.
func TestDispatcher200MarksSucceeded(t *testing.T) {
	t.Parallel()
	repo, store := newTestStorage(t)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, del := makeEndpointAndEnqueue(t, repo, store, srv.URL)
	disp := webhookrun.New(webhookrun.Deps{
		Logger: silentLogger(), Storage: store, Clock: clock.System(), Webhook: repo,
		HTTPClient: srv.Client(), TickInterval: time.Hour, // 우리는 boot 즉시 1회만 사용.
	})
	runDispatcherOnce(t, disp)

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hits = %d, want 1", got)
	}
	got := fetchDelivery(t, repo, store, del.ID)
	if !got.Succeeded {
		t.Errorf("Succeeded = false, want true")
	}
	if got.LastResponseStatus != 200 {
		t.Errorf("LastResponseStatus = %d, want 200", got.LastResponseStatus)
	}
	if got.AttemptCount != 1 {
		t.Errorf("AttemptCount = %d, want 1", got.AttemptCount)
	}
}

// 500 응답이면 attempt_count=1 + next_attempt_at = now + 1m.
func TestDispatcher500RetriesWithBackoff(t *testing.T) {
	t.Parallel()
	repo, store := newTestStorage(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, del := makeEndpointAndEnqueue(t, repo, store, srv.URL)
	before := time.Now().UTC()
	disp := webhookrun.New(webhookrun.Deps{
		Logger: silentLogger(), Storage: store, Clock: clock.System(), Webhook: repo,
		HTTPClient: srv.Client(), TickInterval: time.Hour,
	})
	runDispatcherOnce(t, disp)

	got := fetchDelivery(t, repo, store, del.ID)
	if got.Succeeded {
		t.Errorf("Succeeded = true after 500")
	}
	if got.AttemptCount != 1 {
		t.Errorf("AttemptCount = %d, want 1", got.AttemptCount)
	}
	if got.LastResponseStatus != 500 {
		t.Errorf("LastResponseStatus = %d, want 500", got.LastResponseStatus)
	}
	// next_attempt_at는 약 now+1m. before+30s ~ before+2m 범위.
	gap := got.NextAttemptAt.Sub(before)
	if gap < 30*time.Second || gap > 2*time.Minute {
		t.Errorf("NextAttemptAt gap = %v, want ~1m (between 30s and 2m)", gap)
	}
}

// 네트워크 실패(서버 closed)면 attempt_count++ + last_error 채움.
func TestDispatcherNetworkFailureRetries(t *testing.T) {
	t.Parallel()
	repo, store := newTestStorage(t)

	// listener를 만들고 즉시 close → 연결 reject. URL는 절대 응답 안 함.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // close 후 url로 POST하면 transport error.

	_, del := makeEndpointAndEnqueue(t, repo, store, url)
	disp := webhookrun.New(webhookrun.Deps{
		Logger: silentLogger(), Storage: store, Clock: clock.System(), Webhook: repo,
		HTTPClient: &http.Client{Timeout: 2 * time.Second}, TickInterval: time.Hour,
	})
	runDispatcherOnce(t, disp)

	got := fetchDelivery(t, repo, store, del.ID)
	if got.Succeeded {
		t.Errorf("Succeeded = true after network failure")
	}
	if got.AttemptCount != 1 {
		t.Errorf("AttemptCount = %d, want 1", got.AttemptCount)
	}
	if got.LastError == "" {
		t.Errorf("LastError empty after network failure")
	}
	if got.LastResponseStatus != 0 {
		t.Errorf("LastResponseStatus = %d, want 0 (transport error)", got.LastResponseStatus)
	}
}

// HMAC 헤더(X-Rosshield-Signature)가 본문에 대해 valid해야 합니다.
func TestDispatcherSignsRequestWithHMAC(t *testing.T) {
	t.Parallel()
	repo, store := newTestStorage(t)

	var sigOK int32
	var eventHdr, deliveryHdr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sig := r.Header.Get(webhook.SignatureHeader)
		if webhook.VerifySignature(sig, testSecret, body) {
			atomic.AddInt32(&sigOK, 1)
		}
		eventHdr = r.Header.Get(webhook.EventTypeHeader)
		deliveryHdr = r.Header.Get(webhook.DeliveryIDHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, del := makeEndpointAndEnqueue(t, repo, store, srv.URL)
	disp := webhookrun.New(webhookrun.Deps{
		Logger: silentLogger(), Storage: store, Clock: clock.System(), Webhook: repo,
		HTTPClient: srv.Client(), TickInterval: time.Hour,
	})
	runDispatcherOnce(t, disp)

	if atomic.LoadInt32(&sigOK) != 1 {
		t.Errorf("HMAC verify failed — server saw invalid signature")
	}
	if eventHdr != string(webhook.EventScanCompleted) {
		t.Errorf("X-Rosshield-Event = %q, want scan.completed", eventHdr)
	}
	if deliveryHdr != del.ID {
		t.Errorf("X-Rosshield-Delivery = %q, want %q", deliveryHdr, del.ID)
	}
}

// MaxRetryAttempts 도달한 row는 ListDueDeliveries에서 제외 → POST 발생 안 함.
func TestDispatcherDeadLetterNotRedispatched(t *testing.T) {
	t.Parallel()
	repo, store := newTestStorage(t)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, del := makeEndpointAndEnqueue(t, repo, store, srv.URL)
	now := time.Now().UTC()
	// attempt_count = MaxRetryAttempts → dead-letter 진입.
	if err := store.Bootstrap(context.Background(), func(ctx context.Context, tx storage.Tx) error {
		return repo.MarkDeliveryFailed(ctx, tx, del.ID, webhook.MaxRetryAttempts, now, 500, "max", now)
	}); err != nil {
		t.Fatalf("seed dead-letter: %v", err)
	}

	disp := webhookrun.New(webhookrun.Deps{
		Logger: silentLogger(), Storage: store, Clock: clock.System(), Webhook: repo,
		HTTPClient: srv.Client(), TickInterval: time.Hour,
	})
	runDispatcherOnce(t, disp)

	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("dead-letter dispatched: hits = %d, want 0", got)
	}
}

// disabled endpoint의 delivery는 dead-letter로 진입 (재시도 안 함).
func TestDispatcherDisabledEndpointDeadLetters(t *testing.T) {
	t.Parallel()
	repo, store := newTestStorage(t)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ep, del := makeEndpointAndEnqueue(t, repo, store, srv.URL)
	// endpoint disable.
	tenantCtx := storage.WithTenantID(context.Background(), storage.TenantID(testTenant))
	ep.Enabled = false
	if err := store.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		_, e := repo.UpdateEndpoint(ctx, tx, ep)
		return e
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}

	disp := webhookrun.New(webhookrun.Deps{
		Logger: silentLogger(), Storage: store, Clock: clock.System(), Webhook: repo,
		HTTPClient: srv.Client(), TickInterval: time.Hour,
	})
	runDispatcherOnce(t, disp)

	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("disabled endpoint dispatched: hits = %d, want 0", got)
	}
	got := fetchDelivery(t, repo, store, del.ID)
	if got.AttemptCount != webhook.MaxRetryAttempts {
		t.Errorf("AttemptCount = %d, want MaxRetryAttempts (%d) after disabled dead-letter",
			got.AttemptCount, webhook.MaxRetryAttempts)
	}
	if got.Succeeded {
		t.Errorf("Succeeded = true for disabled endpoint")
	}
}

// Stop은 Run loop를 종료한다 (Done 채널이 닫힘).
func TestDispatcherStopExits(t *testing.T) {
	t.Parallel()
	_, store := newTestStorage(t)
	repo := sqliterepo.New(sqliterepo.Deps{Clock: clock.System(), IDGen: idgen.NewULID()})

	disp := webhookrun.New(webhookrun.Deps{
		Logger: silentLogger(), Storage: store, Clock: clock.System(), Webhook: repo,
		HTTPClient: http.DefaultClient, TickInterval: 50 * time.Millisecond,
	})
	go disp.Run(context.Background())
	time.Sleep(120 * time.Millisecond)
	disp.Stop()

	select {
	case <-disp.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("dispatcher did not exit within 2s after Stop")
	}
}

// === helpers ===

// runDispatcherOnce는 dispatcher.Run을 시작 후 즉시 Stop, dispatchOnce(boot 1회)만 실행합니다.
//
// dispatcher.Run은 부팅 직후 1회 dispatchOnce를 호출하므로, 테스트는 짧게 돌리고 Stop.
func runDispatcherOnce(t *testing.T, disp *webhookrun.Dispatcher) {
	t.Helper()
	go disp.Run(context.Background())
	// Run의 boot dispatchOnce가 끝날 시간 — 단일 delivery 처리는 ms 단위.
	// 50ms는 httptest.Server round-trip + sqlite UPDATE 충분.
	time.Sleep(150 * time.Millisecond)
	disp.Stop()
	select {
	case <-disp.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("dispatcher did not exit within 2s")
	}
}
