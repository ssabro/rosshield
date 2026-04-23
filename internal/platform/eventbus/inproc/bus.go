// Package inproc는 eventbus.Bus의 인프로세스 구현입니다.
//
// 모델 (R2 §1·§3 결정):
//   - 채널 모델 B: subscriber당 전용 channel + fan-out
//   - 고루틴 모델 M2: subscriber당 전용 worker goroutine (구독자 내 순서 보장)
//   - Backpressure: bounded channel + 정책 (Block | DropOldest), 기본 DropOldest + cap 256 + timeout 100ms
//   - Panic 격리: worker recover → log → loop 계속
//
// 참조: docs/design/notes/e1-eventbus-deepdive.md
package inproc

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/idgen"
)

// Deps는 Bus 생성자 의존성입니다.
type Deps struct {
	Logger *slog.Logger
	Clock  clock.Clock
	IDGen  idgen.IDGen
}

// Bus는 인프로세스 EventBus입니다.
type Bus struct {
	deps Deps

	mu     sync.RWMutex
	topics map[string][]*subscription

	closed atomic.Bool
}

// New는 새 Bus를 반환합니다.
func New(deps Deps) *Bus {
	return &Bus{
		deps:   deps,
		topics: make(map[string][]*subscription),
	}
}

// Publish는 evt를 topic 구독자 모두에게 fan-out 합니다.
// 모든 구독자가 channel에 enqueue 완료된 후 nil 반환 (R2-2 수용 보장).
// Bus가 closed면 ErrBusClosed.
func (b *Bus) Publish(ctx context.Context, evt eventbus.Event) error {
	if b.closed.Load() {
		return eventbus.ErrBusClosed
	}
	if evt.Type == "" {
		return eventbus.ErrNoType
	}

	b.fillEnvelope(ctx, &evt)

	subs := b.snapshotSubs(evt.Type)
	for _, sub := range subs {
		if err := sub.enqueue(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}

// fillEnvelope는 비어 있는 envelope 필드를 자동 채웁니다.
func (b *Bus) fillEnvelope(ctx context.Context, evt *eventbus.Event) {
	if evt.ID == "" {
		evt.ID = b.deps.IDGen.New("evt")
	}
	if evt.OccurredAt.IsZero() {
		evt.OccurredAt = b.deps.Clock.Now()
	}
	if evt.CausationID == "" {
		evt.CausationID = eventbus.CausationIDFromContext(ctx)
	}
	if evt.CorrelationID == "" {
		if cid := eventbus.CorrelationIDFromContext(ctx); cid != "" {
			evt.CorrelationID = cid
		} else {
			evt.CorrelationID = b.deps.IDGen.New("cor")
			b.deps.Logger.Debug("eventbus: correlation auto-generated",
				"correlationId", evt.CorrelationID, "type", evt.Type)
		}
	}
}

// snapshotSubs는 publish 동안 mutex를 들고 있지 않도록 구독자 슬라이스를 복사합니다.
func (b *Bus) snapshotSubs(topic string) []*subscription {
	b.mu.RLock()
	defer b.mu.RUnlock()
	subs := b.topics[topic]
	out := make([]*subscription, len(subs))
	copy(out, subs)
	return out
}

// Subscribe는 topic의 새 구독을 등록합니다.
// ctx는 등록 시점 ctx로, handler 호출 ctx와는 별개 (handler ctx는 Bus가 매 호출마다 구성).
func (b *Bus) Subscribe(ctx context.Context, topic string, h eventbus.Handler, opts ...eventbus.SubscribeOption) eventbus.Subscription {
	cfg := eventbus.ApplyOptions(opts)

	sub := &subscription{
		id:       b.deps.IDGen.New("sub"),
		topic:    topic,
		handler:  h,
		cfg:      cfg,
		bus:      b,
		ch:       make(chan eventbus.Event, cfg.Buffer),
		cancelCh: make(chan struct{}),
		doneCh:   make(chan struct{}),
	}

	b.mu.Lock()
	b.topics[topic] = append(b.topics[topic], sub)
	b.mu.Unlock()

	go sub.runWorker()
	return sub
}

// Close는 새 publish를 거부하고 모든 구독을 cancel한 뒤 worker 종료를 대기합니다.
func (b *Bus) Close(ctx context.Context) error {
	if !b.closed.CompareAndSwap(false, true) {
		return nil // 이미 closed
	}

	b.mu.Lock()
	var allSubs []*subscription
	for _, subs := range b.topics {
		allSubs = append(allSubs, subs...)
	}
	b.topics = make(map[string][]*subscription)
	b.mu.Unlock()

	for _, s := range allSubs {
		s.Cancel()
	}
	for _, s := range allSubs {
		select {
		case <-s.Done():
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Drain은 모든 구독자 channel이 비고 in-flight handler가 0이 될 때까지 대기합니다.
// 테스트용 편의 메서드 — 운영 코드에서 호출하지 마십시오.
func (b *Bus) Drain(ctx context.Context) error {
	for {
		if b.allIdle() {
			return nil
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (b *Bus) allIdle() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, subs := range b.topics {
		for _, s := range subs {
			if len(s.ch) > 0 || s.inFlight.Load() > 0 {
				return false
			}
		}
	}
	return true
}

// removeSubscription은 subscription을 topic 목록에서 제거합니다.
// subscription.Cancel()이 호출합니다.
func (b *Bus) removeSubscription(s *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.topics[s.topic]
	for i, x := range subs {
		if x == s {
			b.topics[s.topic] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}
