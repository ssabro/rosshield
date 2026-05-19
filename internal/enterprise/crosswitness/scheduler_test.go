//go:build rosshield_enterprise

// scheduler_test.go — A-1 cross-witness interval fold-in scheduler 단위 테스트.
//
// 본 테스트는 phase7-public-transition-design.md §6.2 spec을 검증합니다:
//   - Scheduler는 일정 interval마다 WitnessProvider에서 다른 테넌트 checkpoint를
//     수집해 ComputeFoldInHash로 새 fold-in hash를 산출하고 OnFoldIn callback을 발사.
//   - Start는 goroutine + ticker 기반, Stop은 graceful (in-flight tick 완료 후 종료).
//   - ctx.Done() 시 즉시 종료, 중복 Start 거부, Stop 후 LastHash 조회 가능.

package crosswitness

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubProvider는 호출마다 미리 정해진 witness slice를 반환하는 테스트 provider입니다.
type stubProvider struct {
	mu        sync.Mutex
	calls     int32
	witnesses []TenantCheckpoint
	err       error
}

func (p *stubProvider) GetWitnesses(_ context.Context) ([]TenantCheckpoint, error) {
	atomic.AddInt32(&p.calls, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return nil, p.err
	}
	out := make([]TenantCheckpoint, len(p.witnesses))
	copy(out, p.witnesses)
	return out, nil
}

func (p *stubProvider) Calls() int32 {
	return atomic.LoadInt32(&p.calls)
}

func newTestWitnesses(ts time.Time) []TenantCheckpoint {
	return []TenantCheckpoint{
		{TenantID: "t-a", Seq: 1, Hash: mkHash(0x11), SignedAt: ts},
		{TenantID: "t-b", Seq: 2, Hash: mkHash(0x22), SignedAt: ts},
	}
}

func TestScheduler_Start_매_interval_마다_callback_발사(t *testing.T) {
	ts := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	provider := &stubProvider{witnesses: newTestWitnesses(ts)}

	var (
		mu        sync.Mutex
		callbacks []Hash
	)
	opts := SchedulerOptions{
		WitnessProvider: provider,
		Interval:        25 * time.Millisecond,
		OnFoldIn: func(_, next Hash, _ []TenantCheckpoint, err error) {
			if err != nil {
				t.Errorf("OnFoldIn 안 에러: %v", err)
				return
			}
			mu.Lock()
			callbacks = append(callbacks, next)
			mu.Unlock()
		},
	}

	s := NewScheduler(opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start 에러: %v", err)
	}

	// 3 tick 이상 기다림.
	time.Sleep(90 * time.Millisecond)
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop 에러: %v", err)
	}

	mu.Lock()
	got := len(callbacks)
	mu.Unlock()
	if got < 2 {
		t.Errorf("callback 발사 횟수 = %d, want ≥ 2", got)
	}
	if provider.Calls() < 2 {
		t.Errorf("provider 호출 횟수 = %d, want ≥ 2", provider.Calls())
	}
}

func TestScheduler_Stop_graceful_종료(t *testing.T) {
	ts := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	provider := &stubProvider{witnesses: newTestWitnesses(ts)}

	s := NewScheduler(SchedulerOptions{
		WitnessProvider: provider,
		Interval:        20 * time.Millisecond,
		OnFoldIn:        func(_, _ Hash, _ []TenantCheckpoint, _ error) {},
	})

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start 에러: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop 에러: %v", err)
	}
	elapsed := time.Since(start)
	// graceful Stop은 in-flight callback이 끝날 때까지 대기 — 단 stub은 빠르므로
	// 100ms 이내로 끝나야 한다.
	if elapsed > 100*time.Millisecond {
		t.Errorf("Stop 지연 = %v, want ≤ 100ms", elapsed)
	}
}

func TestScheduler_Ctx_cancel_시_즉시_종료(t *testing.T) {
	ts := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	provider := &stubProvider{witnesses: newTestWitnesses(ts)}

	doneCh := make(chan struct{})
	s := NewScheduler(SchedulerOptions{
		WitnessProvider: provider,
		Interval:        1 * time.Second, // 의도적으로 길게 — ctx cancel만으로 끝나야.
		OnFoldIn:        func(_, _ Hash, _ []TenantCheckpoint, _ error) {},
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start 에러: %v", err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	go func() {
		_ = s.Stop()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Errorf("ctx cancel 후 200ms 이내 종료 실패")
	}
}

func TestScheduler_LastHash_chain_갱신(t *testing.T) {
	ts := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	provider := &stubProvider{witnesses: newTestWitnesses(ts)}

	var (
		mu       sync.Mutex
		lastPrev Hash
		lastNext Hash
	)
	s := NewScheduler(SchedulerOptions{
		WitnessProvider: provider,
		Interval:        20 * time.Millisecond,
		OnFoldIn: func(prev, next Hash, _ []TenantCheckpoint, _ error) {
			mu.Lock()
			lastPrev = prev
			lastNext = next
			mu.Unlock()
		},
	})

	ctx := context.Background()
	_ = s.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	_ = s.Stop()

	mu.Lock()
	gotPrev := lastPrev
	gotNext := lastNext
	mu.Unlock()

	// 두 번째 이후 tick에서 prev != zero(이전 next가 prev로 진입).
	if gotPrev == (Hash{}) {
		t.Errorf("2번째 tick 이후 prev hash가 zero — chain 갱신 실패")
	}
	if gotNext == (Hash{}) {
		t.Errorf("next hash가 zero — fold-in 산출 실패")
	}
	if s.LastHash() != gotNext {
		t.Errorf("LastHash() = %x, want %x", s.LastHash(), gotNext)
	}
}

func TestScheduler_중복_Start_거부(t *testing.T) {
	provider := &stubProvider{witnesses: nil}
	s := NewScheduler(SchedulerOptions{
		WitnessProvider: provider,
		Interval:        50 * time.Millisecond,
		OnFoldIn:        func(_, _ Hash, _ []TenantCheckpoint, _ error) {},
	})

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("1차 Start 에러: %v", err)
	}
	defer func() { _ = s.Stop() }()

	err := s.Start(ctx)
	if !errors.Is(err, ErrSchedulerAlreadyRunning) {
		t.Errorf("2차 Start err = %v, want ErrSchedulerAlreadyRunning", err)
	}
}

func TestScheduler_NewScheduler_필수_필드_검증(t *testing.T) {
	// WitnessProvider nil 시 Start가 에러를 내야 한다.
	s := NewScheduler(SchedulerOptions{
		Interval: 50 * time.Millisecond,
		OnFoldIn: func(_, _ Hash, _ []TenantCheckpoint, _ error) {},
	})
	err := s.Start(context.Background())
	if !errors.Is(err, ErrSchedulerInvalidOptions) {
		t.Errorf("provider nil Start err = %v, want ErrSchedulerInvalidOptions", err)
	}

	// Interval ≤ 0 시 Start가 에러.
	provider := &stubProvider{}
	s2 := NewScheduler(SchedulerOptions{
		WitnessProvider: provider,
		Interval:        0,
		OnFoldIn:        func(_, _ Hash, _ []TenantCheckpoint, _ error) {},
	})
	err = s2.Start(context.Background())
	if !errors.Is(err, ErrSchedulerInvalidOptions) {
		t.Errorf("Interval 0 Start err = %v, want ErrSchedulerInvalidOptions", err)
	}
}

func TestScheduler_provider_에러_callback_에_전달(t *testing.T) {
	wantErr := errors.New("test provider failure")
	provider := &stubProvider{err: wantErr}

	gotErrCh := make(chan error, 4)
	s := NewScheduler(SchedulerOptions{
		WitnessProvider: provider,
		Interval:        20 * time.Millisecond,
		OnFoldIn: func(_, _ Hash, _ []TenantCheckpoint, err error) {
			gotErrCh <- err
		},
	})

	_ = s.Start(context.Background())
	defer func() { _ = s.Stop() }()

	select {
	case got := <-gotErrCh:
		if !errors.Is(got, wantErr) {
			t.Errorf("callback err = %v, want wrap of %v", got, wantErr)
		}
	case <-time.After(150 * time.Millisecond):
		t.Errorf("150ms 이내 provider 에러 callback 없음")
	}
}
