package inproc

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ssabro/rosshield/internal/platform/eventbus"
)

// subscription은 단일 구독을 표현합니다. eventbus.Subscription을 만족합니다.
type subscription struct {
	id      string
	topic   string
	handler eventbus.Handler
	cfg     eventbus.SubscribeConfig
	bus     *Bus

	ch       chan eventbus.Event
	cancelCh chan struct{}
	doneCh   chan struct{}

	cancelOnce sync.Once
	inFlight   atomic.Int64
}

func (s *subscription) Topic() string         { return s.topic }
func (s *subscription) Done() <-chan struct{} { return s.doneCh }

// Cancel은 idempotent. 두 번째 호출은 no-op (R2 §2).
func (s *subscription) Cancel() {
	s.cancelOnce.Do(func() {
		close(s.cancelCh)
		s.bus.removeSubscription(s)
	})
}

// runWorker는 subscriber 전용 goroutine입니다.
// cancel·channel close 까지 이벤트를 직렬로 처리합니다 (M2 모델).
func (s *subscription) runWorker() {
	defer close(s.doneCh)
	for {
		select {
		case <-s.cancelCh:
			return
		case evt, ok := <-s.ch:
			if !ok {
				return
			}
			s.handle(evt)
		}
	}
}

// handle은 개별 이벤트 핸들링. panic 격리·error 로깅·correlation/causation ctx 주입 (R2 §5·§7).
func (s *subscription) handle(evt eventbus.Event) {
	s.inFlight.Add(1)
	defer s.inFlight.Add(-1)

	defer func() {
		if r := recover(); r != nil {
			s.bus.deps.Logger.Error("eventbus: handler panic",
				"topic", s.topic,
				"subId", s.id,
				"eventId", evt.ID,
				"recovered", fmt.Sprint(r))
		}
	}()

	ctx := context.Background()
	ctx = eventbus.WithCorrelationID(ctx, evt.CorrelationID)
	ctx = eventbus.WithCausationID(ctx, evt.ID) // R2 §7: 직전 이벤트 ID

	if err := s.handler(ctx, evt); err != nil {
		s.bus.deps.Logger.Warn("eventbus: handler error",
			"topic", s.topic,
			"subId", s.id,
			"eventId", evt.ID,
			"err", err.Error())
	}
}

// enqueue는 구독자 channel에 이벤트를 push 합니다. cfg.Overflow 정책에 따라 동작.
func (s *subscription) enqueue(ctx context.Context, evt eventbus.Event) error {
	pubCtx, cancel := context.WithTimeout(ctx, s.cfg.PublishTimeout)
	defer cancel()

	switch s.cfg.Overflow {
	case eventbus.OverflowBlock:
		return s.enqueueBlock(pubCtx, evt)
	case eventbus.OverflowDropOldest:
		return s.enqueueDropOldest(pubCtx, evt)
	default:
		return fmt.Errorf("inproc: unknown overflow policy %d", s.cfg.Overflow)
	}
}

func (s *subscription) enqueueBlock(ctx context.Context, evt eventbus.Event) error {
	select {
	case s.ch <- evt:
		return nil
	case <-s.cancelCh:
		return nil // 취소된 구독자에게는 silently drop.
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *subscription) enqueueDropOldest(ctx context.Context, evt eventbus.Event) error {
	// 1) Fast path.
	select {
	case s.ch <- evt:
		return nil
	default:
	}

	// 2) Channel full — 가장 오래된 1건을 drop.
	select {
	case <-s.ch:
		// dropped one
	case <-s.cancelCh:
		return nil
	case <-ctx.Done():
		return nil // 정책상 drop이므로 에러 반환 안 함.
	}

	// 3) 새 이벤트 push 재시도.
	select {
	case s.ch <- evt:
		return nil
	case <-s.cancelCh:
		return nil
	case <-ctx.Done():
		return nil
	}
}
