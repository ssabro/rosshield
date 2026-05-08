// Package webhookrun은 webhook delivery dispatcher입니다 (E23-B Phase 3).
//
// 책임:
//
//   - 주기적 tick으로 due delivery를 ListDueDeliveries로 조회.
//   - 각 delivery에 대해 endpoint(URL/secret/format) 회수 → HTTP POST.
//   - 응답 2xx면 MarkDeliverySucceeded, 그 외/network error면 MarkDeliveryFailed
//     (next_attempt_at = now + NextRetryDelay(attemptCount)).
//   - attempt_count >= MaxRetryAttempts면 dead-letter (next_attempt_at은 그대로 — ListDueDeliveries에서 자동 제외).
//
// HTTP 요청 헤더:
//
//	Content-Type: application/json | text/plain (CEF는 plain)
//	X-Rosshield-Signature: sha256=<hex>     — HMAC-SHA256(secret, body)
//	X-Rosshield-Event: <EventType>          — 수신자 라우팅 키
//	X-Rosshield-Delivery: <delivery ID>     — 수신자 idempotency 키
//
// 도메인 결합 (P5):
//
//	webhookrun은 domain/integration/webhook 도메인만 import — 다른 도메인 직접 호출 X.
//	bootstrap이 webhook.Service + Storage + Logger를 주입.
//
// 외부 dep 0:
//
//	stdlib net/http + crypto/hmac(webhook.SignPayload 경유)만 사용.
package webhookrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ssabro/rosshield/internal/domain/integration/webhook"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// DefaultTickInterval은 due delivery polling 주기 기본값입니다.
const DefaultTickInterval = 30 * time.Second

// DefaultBatchLimit는 한 tick에 dispatch하는 delivery 최대 수입니다.
const DefaultBatchLimit = 50

// DefaultHTTPTimeout은 단일 webhook POST 타임아웃입니다.
const DefaultHTTPTimeout = 30 * time.Second

// Deps는 Dispatcher 의존성입니다.
type Deps struct {
	Logger  *slog.Logger
	Storage storage.Storage
	Clock   clock.Clock
	Webhook webhook.Service

	// HTTPClient는 webhook POST 클라이언트 (테스트에서 stub 가능). nil이면 default(net/http) 생성.
	HTTPClient *http.Client

	// TickInterval은 due polling 주기. 0이면 DefaultTickInterval.
	TickInterval time.Duration

	// BatchLimit은 한 tick에 처리할 최대 delivery 수. 0이면 DefaultBatchLimit.
	BatchLimit int
}

// Dispatcher는 due delivery를 polling 방식으로 dispatch하는 background worker입니다.
//
// Run은 ctx 만료 또는 Stop 호출 시 깨끗이 종료. 동시에 1 인스턴스만 가정 (multi-replica는 후속 epic).
type Dispatcher struct {
	deps Deps

	stopOnce sync.Once
	stopped  chan struct{}
	doneCh   chan struct{}
}

// New는 새 Dispatcher를 반환합니다.
func New(deps Deps) *Dispatcher {
	if deps.TickInterval <= 0 {
		deps.TickInterval = DefaultTickInterval
	}
	if deps.BatchLimit <= 0 {
		deps.BatchLimit = DefaultBatchLimit
	}
	if deps.HTTPClient == nil {
		deps.HTTPClient = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Clock == nil {
		deps.Clock = clock.System()
	}
	return &Dispatcher{
		deps:    deps,
		stopped: make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// Run은 ticker 기반으로 dispatchOnce를 반복 호출하다가 ctx 만료 또는 Stop 시 종료합니다.
//
// 호출자는 별도 goroutine에서 호출하고, Stop/ctx cancel 후 Done()으로 graceful 종료 대기.
// 단일 인스턴스 가정 — 같은 Dispatcher.Run 동시 호출은 미정의.
func (d *Dispatcher) Run(ctx context.Context) {
	defer close(d.doneCh)

	// 부팅 직후 즉시 1회 dispatch — 미처리 backlog 빨리 소진.
	d.dispatchOnce(ctx)

	ticker := time.NewTicker(d.deps.TickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.deps.Logger.Info("webhookrun: ctx cancelled, exiting")
			return
		case <-d.stopped:
			d.deps.Logger.Info("webhookrun: stop requested, exiting")
			return
		case <-ticker.C:
			d.dispatchOnce(ctx)
		}
	}
}

// Stop은 Run loop를 종료 신호합니다 (idempotent). Done()으로 종료 대기.
func (d *Dispatcher) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopped)
	})
}

// Done은 Run loop가 종료될 때 닫히는 채널을 반환합니다.
func (d *Dispatcher) Done() <-chan struct{} {
	return d.doneCh
}

// dispatchOnce는 한 tick의 작업을 수행합니다 — due 회수 + 각 delivery dispatch.
//
// 단일 Bootstrap Tx에서 due 회수 후, 각 delivery는 별도 호출(POST + mark)로 처리.
// 한 delivery 실패가 다음 delivery를 막지 않음 — best-effort.
func (d *Dispatcher) dispatchOnce(ctx context.Context) {
	now := d.deps.Clock.Now().UTC()
	var due []webhook.WebhookDelivery
	if err := d.deps.Storage.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		out, e := d.deps.Webhook.ListDueDeliveries(ctx, tx, now, d.deps.BatchLimit)
		due = out
		return e
	}); err != nil {
		d.deps.Logger.Warn("webhookrun: list due failed", "err", err.Error())
		return
	}
	if len(due) == 0 {
		return
	}
	d.deps.Logger.Debug("webhookrun: due deliveries", "count", len(due))

	for _, del := range due {
		if ctx.Err() != nil {
			return
		}
		d.processOne(ctx, del)
	}
}

// processOne은 1건의 delivery를 dispatch합니다 — endpoint 조회 + POST + mark.
//
// endpoint가 사라졌거나 disabled면 즉시 fail mark (재시도하지 않도록 attempt_count = MaxRetryAttempts로 진입).
func (d *Dispatcher) processOne(ctx context.Context, del webhook.WebhookDelivery) {
	now := d.deps.Clock.Now().UTC()

	// endpoint 조회는 tenant scope에서 — delivery에 tenant_id가 박혀있음.
	tenantCtx := storage.WithTenantID(ctx, del.TenantID)
	var ep webhook.WebhookEndpoint
	if err := d.deps.Storage.Tx(tenantCtx, func(ctx context.Context, tx storage.Tx) error {
		out, e := d.deps.Webhook.GetEndpoint(ctx, tx, del.EndpointID)
		ep = out
		return e
	}); err != nil {
		// endpoint가 사라졌으면 dead-letter로 진입시켜 다시 시도하지 않도록 함.
		d.deps.Logger.Warn("webhookrun: get endpoint failed — dead-letter",
			"deliveryId", del.ID, "endpointId", del.EndpointID, "err", err.Error())
		_ = d.markFailed(ctx, del.ID, webhook.MaxRetryAttempts, now, 0, "endpoint unavailable: "+err.Error(), now)
		return
	}
	if !ep.Enabled {
		d.deps.Logger.Info("webhookrun: endpoint disabled — dead-letter",
			"deliveryId", del.ID, "endpointId", del.EndpointID)
		_ = d.markFailed(ctx, del.ID, webhook.MaxRetryAttempts, now, 0, "endpoint disabled", now)
		return
	}

	status, err := d.postOnce(ctx, ep, del)
	attempt := del.AttemptCount + 1
	if err == nil && status >= 200 && status < 300 {
		d.deps.Logger.Info("webhookrun: delivery success",
			"deliveryId", del.ID, "endpointId", ep.ID, "status", status, "attempt", attempt)
		if mErr := d.markSucceeded(ctx, del.ID, attempt, status, now); mErr != nil {
			d.deps.Logger.Warn("webhookrun: mark succeeded failed",
				"deliveryId", del.ID, "err", mErr.Error())
		}
		return
	}

	// 실패 path — attempt_count++, next_attempt_at 계산.
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else {
		errMsg = fmt.Sprintf("non-2xx status: %d", status)
	}

	next := now
	if delay, ok := webhook.NextRetryDelay(attempt); ok {
		next = now.Add(delay)
	}
	// attempt가 Max 도달이면 next는 그대로 now → ListDueDeliveries가 attempt_count 조건으로 자동 제외.
	d.deps.Logger.Info("webhookrun: delivery failed",
		"deliveryId", del.ID, "endpointId", ep.ID, "status", status, "attempt", attempt,
		"nextAt", next.Format(time.RFC3339), "err", errMsg)

	if mErr := d.markFailed(ctx, del.ID, attempt, next, status, errMsg, now); mErr != nil {
		d.deps.Logger.Warn("webhookrun: mark failed update failed",
			"deliveryId", del.ID, "err", mErr.Error())
	}
}

// postOnce는 endpoint URL로 단일 HTTP POST를 수행합니다.
//
// 반환: (HTTP status, error). transport error면 status=0, response body 무시.
// PingResult는 PingEndpoint 결과입니다 (E29 — webhook test CLI).
type PingResult struct {
	Status    int    // HTTP status code (0 = transport error)
	Error     string // transport·sign 에러 메시지 (성공 시 빈 값)
	LatencyMs int64  // POST 호출 wall-clock 소요
}

// PingEndpoint는 endpoint에 1회 ping payload를 POST하고 결과를 반환합니다.
//
// delivery row를 INSERT하지 않음 — 운영자 ad-hoc 검증 용도. 결과 metric counter는
// 증가 안 함(test 호출은 운영 baseline 오염 회피).
//
// tenantCtx로 endpoint 조회 → postOnce 흐름 재사용. endpoint 미존재면 webhook.ErrEndpointNotFound.
func (d *Dispatcher) PingEndpoint(ctx context.Context, endpointID string) (PingResult, error) {
	tenantID := storage.TenantIDFromContext(ctx)
	if tenantID == "" {
		return PingResult{}, storage.ErrTenantMissing
	}
	var ep webhook.WebhookEndpoint
	if err := d.deps.Storage.Tx(ctx, func(c context.Context, tx storage.Tx) error {
		out, e := d.deps.Webhook.GetEndpoint(c, tx, endpointID)
		if e != nil {
			return e
		}
		ep = out
		return nil
	}); err != nil {
		return PingResult{}, err
	}

	// 단순 ping payload — 수신자가 routing 가능한 minimal JSON.
	pingDel := webhook.WebhookDelivery{
		ID:        "ping_" + endpointID,
		EventType: webhook.EventScanCompleted, // 라우팅 키 — 사용자가 알 수 있는 값으로 통일
		Payload:   []byte(`{"ping":true,"source":"rosshield-webhook-test"}`),
	}

	start := d.deps.Clock.Now()
	status, err := d.postOnce(ctx, ep, pingDel)
	latency := d.deps.Clock.Now().Sub(start).Milliseconds()
	res := PingResult{Status: status, LatencyMs: latency}
	if err != nil {
		res.Error = err.Error()
	}
	return res, nil
}

func (d *Dispatcher) postOnce(ctx context.Context, ep webhook.WebhookEndpoint, del webhook.WebhookDelivery) (int, error) {
	body := del.Payload
	sig, err := webhook.SignPayload(ep.Secret, body)
	if err != nil {
		return 0, fmt.Errorf("sign: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", contentTypeFor(ep.Format))
	req.Header.Set(webhook.SignatureHeader, sig)
	req.Header.Set(webhook.EventTypeHeader, string(del.EventType))
	req.Header.Set(webhook.DeliveryIDHeader, del.ID)
	req.Header.Set("User-Agent", "rosshield-webhook/1.0")

	resp, err := d.deps.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	return resp.StatusCode, nil
}

// markSucceeded는 Bootstrap Tx로 MarkDeliverySucceeded를 호출합니다 (cross-tenant write).
func (d *Dispatcher) markSucceeded(ctx context.Context, deliveryID string, attempt, status int, when time.Time) error {
	return d.deps.Storage.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		return d.deps.Webhook.MarkDeliverySucceeded(ctx, tx, deliveryID, attempt, status, when)
	})
}

// markFailed는 Bootstrap Tx로 MarkDeliveryFailed를 호출합니다 (cross-tenant write).
func (d *Dispatcher) markFailed(ctx context.Context, deliveryID string, attempt int, next time.Time, status int, errMsg string, when time.Time) error {
	if err := d.deps.Storage.Bootstrap(ctx, func(ctx context.Context, tx storage.Tx) error {
		return d.deps.Webhook.MarkDeliveryFailed(ctx, tx, deliveryID, attempt, next, status, errMsg, when)
	}); err != nil {
		// 미존재면 silent — 이미 다른 인스턴스가 처리했을 가능성.
		if errors.Is(err, webhook.ErrDeliveryNotFound) {
			return nil
		}
		return err
	}
	return nil
}

// contentTypeFor는 endpoint format에 따른 Content-Type 헤더 값을 반환합니다.
//
//	json/ecs → application/json
//	cef      → text/plain (CEF는 단일 라인 텍스트)
//	그 외     → application/octet-stream (안전한 fallback)
func contentTypeFor(f webhook.Format) string {
	switch f {
	case webhook.PayloadFormatJSON, webhook.PayloadFormatECS:
		return "application/json"
	case webhook.PayloadFormatCEF:
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
