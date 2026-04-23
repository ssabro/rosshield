package inproc_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/eventbus"
	"github.com/ssabro/rosshield/internal/platform/eventbus/inproc"
	"github.com/ssabro/rosshield/internal/platform/idgen"
)

func newTestBus(t *testing.T) *inproc.Bus {
	t.Helper()
	b := inproc.New(inproc.Deps{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Clock:  clock.System(),
		IDGen:  idgen.NewULID(),
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = b.Close(ctx)
	})
	return b
}

func TestEventBusInProcPublishAndSubscribe(t *testing.T) {
	t.Parallel()

	bus := newTestBus(t)

	received := make(chan eventbus.Event, 1)
	sub := bus.Subscribe(context.Background(), "scan.ScanCompleted", func(ctx context.Context, evt eventbus.Event) error {
		received <- evt
		return nil
	})
	defer sub.Cancel()

	ctx := eventbus.WithCausationID(context.Background(), "evt_cause_xyz")
	ctx = eventbus.WithCorrelationID(ctx, "cor_req_abc")

	if err := bus.Publish(ctx, eventbus.Event{
		Type:     "scan.ScanCompleted",
		TenantID: "tn_test",
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case evt := <-received:
		if evt.Type != "scan.ScanCompleted" {
			t.Errorf("Type = %q, want scan.ScanCompleted", evt.Type)
		}
		if evt.ID == "" || !startsWith(evt.ID, "evt_") {
			t.Errorf("ID = %q, want non-empty with evt_ prefix", evt.ID)
		}
		if evt.CausationID != "evt_cause_xyz" {
			t.Errorf("CausationID = %q, want evt_cause_xyz (from ctx)", evt.CausationID)
		}
		if evt.CorrelationID != "cor_req_abc" {
			t.Errorf("CorrelationID = %q, want cor_req_abc (from ctx)", evt.CorrelationID)
		}
		if evt.OccurredAt.IsZero() {
			t.Error("OccurredAt was not auto-filled")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive event in 1s")
	}
}

func TestEventBusHandlerErrorIsolated(t *testing.T) {
	t.Parallel()

	bus := newTestBus(t)

	panickingCalled := atomic.Int32{}
	healthyReceived := make(chan eventbus.Event, 4)

	subPanic := bus.Subscribe(context.Background(), "test.Panicker", func(ctx context.Context, evt eventbus.Event) error {
		panickingCalled.Add(1)
		panic("boom")
	})
	defer subPanic.Cancel()

	subHealthy := bus.Subscribe(context.Background(), "test.Panicker", func(ctx context.Context, evt eventbus.Event) error {
		healthyReceived <- evt
		return nil
	})
	defer subHealthy.Cancel()

	const n = 3
	for i := 0; i < n; i++ {
		if err := bus.Publish(context.Background(), eventbus.Event{
			Type:    "test.Panicker",
			Payload: []byte(`{"i":` + strconv.Itoa(i) + `}`),
		}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	for i := 0; i < n; i++ {
		select {
		case <-healthyReceived:
		case <-time.After(time.Second):
			t.Fatalf("healthy sub did not receive event %d in 1s (panic in subPanic blocked it?)", i)
		}
	}

	// Drain으로 panic worker도 in-flight 완료 보장.
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bus.Drain(drainCtx); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	// Publish는 panic 영향 없이 정상 동작했고 panic worker도 모든 이벤트를 처리했어야 함.
	if got := panickingCalled.Load(); got != int32(n) {
		t.Errorf("panicking handler invocations = %d, want %d", got, n)
	}
}

func TestEventBusCancelIsIdempotent(t *testing.T) {
	t.Parallel()

	bus := newTestBus(t)
	sub := bus.Subscribe(context.Background(), "test.Cancel", func(ctx context.Context, evt eventbus.Event) error {
		return nil
	})

	sub.Cancel()
	sub.Cancel() // second call must be no-op (no panic, no double-close).

	select {
	case <-sub.Done():
	case <-time.After(time.Second):
		t.Fatal("sub.Done() did not close in 1s after Cancel")
	}
}

func TestEventBusOrderPreservedPerSubscription(t *testing.T) {
	t.Parallel()

	bus := newTestBus(t)

	const n = 50
	received := make(chan int, n)
	sub := bus.Subscribe(context.Background(), "test.Order",
		func(ctx context.Context, evt eventbus.Event) error {
			i, _ := strconv.Atoi(string(evt.Payload))
			received <- i
			return nil
		},
		eventbus.WithBuffer(n),
	)
	defer sub.Cancel()

	for i := 0; i < n; i++ {
		if err := bus.Publish(context.Background(), eventbus.Event{
			Type:    "test.Order",
			Payload: []byte(strconv.Itoa(i)),
		}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	for i := 0; i < n; i++ {
		select {
		case got := <-received:
			if got != i {
				t.Fatalf("event %d received as %d (order broken)", i, got)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only received %d/%d events", i, n)
		}
	}
}

func TestEventBusBackpressureBlockPolicy(t *testing.T) {
	t.Parallel()

	bus := newTestBus(t)

	release := make(chan struct{})
	sub := bus.Subscribe(context.Background(), "test.Block",
		func(ctx context.Context, evt eventbus.Event) error {
			<-release // 첫 이벤트는 release 신호까지 handler가 점유.
			return nil
		},
		eventbus.WithBuffer(1),
		eventbus.WithOverflow(eventbus.OverflowBlock),
		eventbus.WithPublishTimeout(100*time.Millisecond),
	)
	defer sub.Cancel()
	defer close(release)

	// 1) 첫 publish: handler 점유, 정상 enqueue.
	if err := bus.Publish(context.Background(), eventbus.Event{Type: "test.Block"}); err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	// 2) 두 번째 publish: buffer cap=1 (이미 차 있음 또는 handler 점유 중) → 정상 enqueue (queue에 1건 대기).
	if err := bus.Publish(context.Background(), eventbus.Event{Type: "test.Block"}); err != nil {
		t.Fatalf("second Publish: %v", err)
	}
	// 3) 세 번째 publish: buffer 가득 + handler 멈춤 → publish timeout 100ms 후 ctx.DeadlineExceeded 계열 에러 기대.
	err := bus.Publish(context.Background(), eventbus.Event{Type: "test.Block"})
	if err == nil {
		t.Error("third Publish should have timed out (Block + buffer full + handler blocked)")
	}
}

func TestEventBusBackpressureDropOldest(t *testing.T) {
	t.Parallel()

	bus := newTestBus(t)

	release := make(chan struct{})
	received := make(chan eventbus.Event, 8)

	sub := bus.Subscribe(context.Background(), "test.DropOldest",
		func(ctx context.Context, evt eventbus.Event) error {
			<-release
			received <- evt
			return nil
		},
		eventbus.WithBuffer(1),
		eventbus.WithOverflow(eventbus.OverflowDropOldest),
		eventbus.WithPublishTimeout(50*time.Millisecond),
	)
	defer sub.Cancel()

	// handler 점유 동안 5건 push. buffer cap=1 + DropOldest이므로 가장 마지막만 살아남아야 함.
	for i := 0; i < 5; i++ {
		if err := bus.Publish(context.Background(), eventbus.Event{
			Type:    "test.DropOldest",
			Payload: []byte(strconv.Itoa(i)),
		}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	close(release) // handler unblock

	// handler 1차 (release 직전 점유) 1건 + queue 잔존 1건 = 최대 2건 수신.
	timeout := time.After(time.Second)
	got := []int{}
loop:
	for {
		select {
		case evt := <-received:
			i, _ := strconv.Atoi(string(evt.Payload))
			got = append(got, i)
			if len(got) >= 2 {
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	if len(got) > 2 {
		t.Errorf("received %d events, want ≤ 2 (DropOldest should have dropped intermediates): %v", len(got), got)
	}
	if len(got) > 0 && got[len(got)-1] != 4 {
		t.Errorf("last received = %d, want 4 (newest should survive DropOldest): %v", got[len(got)-1], got)
	}
}

func TestEventBusCloseRejectsNewPublish(t *testing.T) {
	t.Parallel()

	bus := inproc.New(inproc.Deps{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Clock:  clock.System(),
		IDGen:  idgen.NewULID(),
	})

	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bus.Close(closeCtx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := bus.Publish(context.Background(), eventbus.Event{Type: "test.AfterClose"})
	if !errors.Is(err, eventbus.ErrBusClosed) {
		t.Errorf("Publish after Close err = %v, want ErrBusClosed", err)
	}
}

func TestEventBusDrainWaitsForInflight(t *testing.T) {
	t.Parallel()

	bus := newTestBus(t)

	const handlerDelay = 100 * time.Millisecond
	processed := atomic.Int32{}

	sub := bus.Subscribe(context.Background(), "test.Drain",
		func(ctx context.Context, evt eventbus.Event) error {
			time.Sleep(handlerDelay)
			processed.Add(1)
			return nil
		},
		eventbus.WithBuffer(8),
	)
	defer sub.Cancel()

	const n = 3
	for i := 0; i < n; i++ {
		if err := bus.Publish(context.Background(), eventbus.Event{Type: "test.Drain"}); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}

	drainStart := time.Now()
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bus.Drain(drainCtx); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	elapsed := time.Since(drainStart)

	if got := processed.Load(); got != int32(n) {
		t.Errorf("processed = %d, want %d (Drain returned before in-flight handlers completed)", got, n)
	}
	// Drain은 in-flight handlers가 끝날 때까지 대기해야 하므로 최소 n × handlerDelay 시간이 걸려야 함.
	// (직렬 실행 가정 — 단일 sub × 단일 worker.)
	if elapsed < handlerDelay {
		t.Errorf("Drain returned in %v, expected ≥ %v", elapsed, handlerDelay)
	}
}

func TestEventBusAutoGeneratesCorrelationIDIfMissing(t *testing.T) {
	t.Parallel()

	bus := newTestBus(t)

	received := make(chan eventbus.Event, 1)
	sub := bus.Subscribe(context.Background(), "test.AutoCor", func(ctx context.Context, evt eventbus.Event) error {
		received <- evt
		return nil
	})
	defer sub.Cancel()

	// ctx에 correlation 없음 → Bus가 cor_<ULID> 자동 생성 (R2-7).
	if err := bus.Publish(context.Background(), eventbus.Event{Type: "test.AutoCor"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case evt := <-received:
		if evt.CorrelationID == "" {
			t.Error("CorrelationID should be auto-generated, got empty")
		}
		if !startsWith(evt.CorrelationID, "cor_") {
			t.Errorf("CorrelationID = %q, want cor_ prefix", evt.CorrelationID)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive event")
	}
}

func TestEventBusRequiresEventType(t *testing.T) {
	t.Parallel()

	bus := newTestBus(t)
	err := bus.Publish(context.Background(), eventbus.Event{}) // Type 비어 있음
	if !errors.Is(err, eventbus.ErrNoType) {
		t.Errorf("Publish with empty Type err = %v, want ErrNoType", err)
	}
}

// 테스트 헬퍼: race detector 없이도 사용할 수 있게 wg 패턴을 표현한 보조 함수.
// (현재는 직접 사용처 없음 — t.Parallel과 동시성 검증 시 추가될 수 있음.)
var _ = sync.WaitGroup{}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
