package webhookrun

// eventbridge.go — E23-D EventBus → webhook.Service.Enqueue bridge.
//
// 책임:
//
//	EventBus의 도메인 이벤트(scan.completed·insight.created·audit.checkpoint)를
//	구독해 webhook.Service.Enqueue(domainEvent)로 매핑·영속한다.
//	실 HTTP 송출은 별도 Process worker(Dispatcher) 책임 — 본 bridge는 큐잉만.
//
// 도메인 결합 (P5):
//
//	bridge는 webhook 도메인 + EventBus 인터페이스만 import. 원천 도메인(scan/insight/audit)을
//	직접 import하지 않고, 이벤트의 Type 문자열만 가지고 webhook.EventType으로 매핑.
//
// EventType 매핑:
//
//	"scan.completed"   → webhook.EventScanCompleted
//	"insight.created"  → webhook.EventInsightCreated
//	"audit.checkpoint" → webhook.EventAuditCheckpoint
//	그 외             → skip (구독 자체를 안 하므로 실제 도달 안 함).
//
// 멀티테넌시:
//
//	각 EventBus.Event는 TenantID를 캐리. 본 bridge는 tenant ctx를 세팅한 후 Storage.Tx를
//	호출 — webhook.Enqueue가 tenant scope endpoint만 매칭.
//
// 옵트인:
//
//	bridge.Start()를 호출하지 않으면 구독 0개 — 모든 도메인 이벤트가 webhook으로 흘러들어가지 않음.
//	bootstrap이 enable 결정.

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ssabro/rosshield/internal/domain/integration/webhook"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// BridgeDeps는 EventBridge 의존성입니다.
type BridgeDeps struct {
	Logger  *slog.Logger
	Storage storage.Storage
	Webhook webhook.Service
}

// EventBridge는 EventBus 구독 → webhook 영속의 결선 글루입니다.
//
// 멀티 topic 구독 — Stop 시 모두 cancel.
type EventBridge struct {
	deps BridgeDeps
	subs []eventbus.Subscription
}

// NewBridge는 새 EventBridge를 반환합니다.
func NewBridge(deps BridgeDeps) *EventBridge {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &EventBridge{deps: deps}
}

// subscribedTopics는 본 bridge가 구독하는 EventBus topic 목록입니다.
//
// 새 topic 추가는 본 슬라이스에 한 줄 + topicToWebhookEventType에 매핑 추가만으로 가능.
var subscribedTopics = []string{
	"scan.completed",
	"insight.created",
	"audit.checkpoint",
}

// topicToWebhookEventType는 EventBus topic을 webhook.EventType으로 매핑합니다.
//
// 미지원 topic은 빈 문자열 반환 — handler가 skip.
func topicToWebhookEventType(topic string) webhook.EventType {
	switch topic {
	case "scan.completed":
		return webhook.EventScanCompleted
	case "insight.created":
		return webhook.EventInsightCreated
	case "audit.checkpoint":
		return webhook.EventAuditCheckpoint
	default:
		return ""
	}
}

// Start는 모든 subscribedTopics에 대해 EventBus 구독을 시작합니다.
//
// 호출 시점: bootstrap 결선 후, Shutdown 전까지 1회. 두 번째 호출은 panic 방지를 위해 무시.
func (b *EventBridge) Start(ctx context.Context, bus eventbus.Bus) {
	if len(b.subs) > 0 {
		return
	}
	for _, topic := range subscribedTopics {
		topic := topic
		sub := bus.Subscribe(ctx, topic, func(ctx context.Context, evt eventbus.Event) error {
			return b.handle(ctx, topic, evt)
		})
		b.subs = append(b.subs, sub)
	}
	b.deps.Logger.Info("webhook event bridge started",
		"topics", subscribedTopics)
}

// Stop은 모든 구독을 cancel하고 worker 종료를 대기합니다 (idempotent).
func (b *EventBridge) Stop() {
	for _, sub := range b.subs {
		sub.Cancel()
	}
	for _, sub := range b.subs {
		<-sub.Done()
	}
	b.subs = nil
}

// handle은 단일 EventBus event를 webhook.Service.Enqueue로 전달합니다.
//
// 본 핸들러는 EventBus subscription worker goroutine에서 호출됨. error 반환은
// EventBus가 warning log만 — 본 bridge는 실패 시 영속 X (delivery 영속 자체 실패는
// dispatcher가 다음 tick에 재시도 못함 — application layer에서 alert 결정).
func (b *EventBridge) handle(ctx context.Context, topic string, evt eventbus.Event) error {
	whType := topicToWebhookEventType(topic)
	if whType == "" {
		// 코드 정합성 — 등록되지 않은 topic이 도달하면 silently skip.
		return nil
	}
	if evt.TenantID == "" {
		b.deps.Logger.Warn("webhook bridge: skipping event with empty tenantID",
			"eventId", evt.ID, "topic", topic)
		return nil
	}

	domainEvt := webhook.DomainEvent{
		EventID:       evt.ID,
		TenantID:      storage.TenantID(evt.TenantID),
		Type:          whType,
		OccurredAt:    evt.OccurredAt,
		Payload:       evt.Payload,
		AggregateType: evt.Aggregate.Type,
		AggregateID:   evt.Aggregate.ID,
	}

	// tenant scope tx — webhook.Enqueue가 tenant scope endpoint만 lookup.
	tenantCtx := storage.WithTenantID(ctx, domainEvt.TenantID)
	err := b.deps.Storage.Tx(tenantCtx, func(c context.Context, tx storage.Tx) error {
		_, e := b.deps.Webhook.Enqueue(c, tx, domainEvt)
		return e
	})
	if err != nil {
		// ErrTenantMissing은 호출 측 결선 결함 — 운영자가 봐야 함.
		if errors.Is(err, storage.ErrTenantMissing) {
			b.deps.Logger.Error("webhook bridge: tenant missing in tx",
				"eventId", evt.ID, "topic", topic, "tenantId", evt.TenantID)
			return err
		}
		b.deps.Logger.Warn("webhook bridge: enqueue failed",
			"eventId", evt.ID, "topic", topic, "err", err.Error())
		return err
	}
	return nil
}
