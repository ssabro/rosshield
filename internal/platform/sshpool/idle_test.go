package sshpool_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/platform/sshpool"
	"github.com/ssabro/rosshield/internal/platform/sshpool/sshpooltest"
)

// idle_test.go — Pool idle 재사용 + metrics 단위 테스트 (scanrun SSH 통합 Stage 4).
//
// design doc `docs/design/notes/scanrun-ssh-integration-design.md` §6 Stage 4 검증:
//   - idle hit/miss (IdleTimeout > 0 시 release된 conn 다음 Acquire에서 재사용)
//   - 만료 eviction (IdleTimeout 초과 conn은 Acquire 시 close + skip)
//   - keepalive failure 시 close (죽은 conn은 evictExpiredAndDead가 정리)
//   - dial_total / idle_conns metric 카운트

// recordingPoolMetrics는 단위 테스트용 PoolMetrics 구현입니다.
type recordingPoolMetrics struct {
	mu          sync.Mutex
	dialOK      int
	dialFail    int
	idleGauge   int // 마지막 set 값
	gaugeWrites int // set 호출 횟수
}

func (r *recordingPoolMetrics) IncDial(result string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch result {
	case "ok":
		r.dialOK++
	case "fail":
		r.dialFail++
	}
}

func (r *recordingPoolMetrics) SetIdleConns(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.idleGauge = n
	r.gaugeWrites++
}

func (r *recordingPoolMetrics) snapshot() (dialOK, dialFail, gauge int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dialOK, r.dialFail, r.idleGauge
}

func TestPool_IdleReuse_HitAfterRelease(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, func(cmd string) sshpooltest.ExecResponse {
		return sshpooltest.ExecResponse{ExitCode: 0}
	})

	rec := &recordingPoolMetrics{}
	p := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:      5,
		DialMaxRetries:    0,
		IdleTimeout:       30 * time.Second,
		KeepaliveInterval: 1 * time.Hour, // 테스트 중 keepalive goroutine 발동 회피
		Metrics:           rec,
	})
	t.Cleanup(func() { _ = p.Close() })

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}
	key := sshpool.PoolKey{TenantID: "tn", KeyID: "kek", Host: srv.Host, Port: srv.Port}

	// 1차 Acquire — dial 1회.
	c1, r1, err := p.Acquire(context.Background(), key, target)
	if err != nil {
		t.Fatalf("Acquire1: %v", err)
	}
	r1()

	// 2차 Acquire — idle hit 예상, dial 추가 0.
	c2, r2, err := p.Acquire(context.Background(), key, target)
	if err != nil {
		t.Fatalf("Acquire2: %v", err)
	}
	r2()

	dialOK, dialFail, _ := rec.snapshot()
	if dialOK != 1 {
		t.Errorf("dial_total{result=ok} = %d, want 1 (2차는 idle hit)", dialOK)
	}
	if dialFail != 0 {
		t.Errorf("dial_total{result=fail} = %d, want 0", dialFail)
	}
	// 1차·2차 client는 같은 conn (LIFO 재사용).
	if c1 != c2 {
		t.Errorf("idle reuse expected same client pointer (LIFO)")
	}
}

func TestPool_IdleReuse_DisabledFallsBackToDial(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, nil)

	rec := &recordingPoolMetrics{}
	p := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:   5,
		DialMaxRetries: 0,
		// IdleTimeout=0 — idle 재사용 비활성 (Phase 1 호환 모드).
		Metrics: rec,
	})
	t.Cleanup(func() { _ = p.Close() })

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}
	key := sshpool.PoolKey{TenantID: "tn", KeyID: "kek", Host: srv.Host, Port: srv.Port}

	// 두 번 Acquire — 둘 다 dial 발생 예상.
	for i := 0; i < 2; i++ {
		_, release, err := p.Acquire(context.Background(), key, target)
		if err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
		release()
	}

	dialOK, _, _ := rec.snapshot()
	if dialOK != 2 {
		t.Errorf("dial_total{result=ok} = %d, want 2 (idle 비활성)", dialOK)
	}
}

func TestPool_IdleEviction_AfterTimeout(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, nil)

	rec := &recordingPoolMetrics{}
	p := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:      5,
		DialMaxRetries:    0,
		IdleTimeout:       50 * time.Millisecond, // 짧은 timeout으로 만료 시뮬레이션
		KeepaliveInterval: 1 * time.Hour,
		Metrics:           rec,
	})
	t.Cleanup(func() { _ = p.Close() })

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}
	key := sshpool.PoolKey{TenantID: "tn", KeyID: "kek", Host: srv.Host, Port: srv.Port}

	// 1차 Acquire + release → idle.
	_, r1, err := p.Acquire(context.Background(), key, target)
	if err != nil {
		t.Fatalf("Acquire1: %v", err)
	}
	r1()

	// IdleTimeout 초과 대기.
	time.Sleep(120 * time.Millisecond)

	// 2차 Acquire — idle 만료 → 새 dial 예상.
	_, r2, err := p.Acquire(context.Background(), key, target)
	if err != nil {
		t.Fatalf("Acquire2: %v", err)
	}
	r2()

	dialOK, _, _ := rec.snapshot()
	if dialOK != 2 {
		t.Errorf("dial_total{result=ok} = %d, want 2 (만료 후 새 dial)", dialOK)
	}
}

func TestPool_IdleConnsGauge_TracksPushPop(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, nil)

	rec := &recordingPoolMetrics{}
	p := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:      5,
		DialMaxRetries:    0,
		IdleTimeout:       30 * time.Second,
		KeepaliveInterval: 1 * time.Hour,
		Metrics:           rec,
	})
	t.Cleanup(func() { _ = p.Close() })

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}
	key := sshpool.PoolKey{TenantID: "tn", KeyID: "kek", Host: srv.Host, Port: srv.Port}

	// 1차 Acquire — gauge 0 유지(idle 없음).
	_, r1, err := p.Acquire(context.Background(), key, target)
	if err != nil {
		t.Fatalf("Acquire1: %v", err)
	}
	r1() // → idle 1.

	_, _, gauge := rec.snapshot()
	if gauge != 1 {
		t.Errorf("idle gauge after release = %d, want 1", gauge)
	}

	// 2차 Acquire — idle pop → gauge 0.
	_, r2, err := p.Acquire(context.Background(), key, target)
	if err != nil {
		t.Fatalf("Acquire2: %v", err)
	}
	_, _, gauge = rec.snapshot()
	if gauge != 0 {
		t.Errorf("idle gauge after pop = %d, want 0", gauge)
	}
	r2() // → idle 1 다시.

	_, _, gauge = rec.snapshot()
	if gauge != 1 {
		t.Errorf("idle gauge after second release = %d, want 1", gauge)
	}
}

func TestPool_Close_ClosesIdleConns(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, nil)

	rec := &recordingPoolMetrics{}
	p := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:      5,
		DialMaxRetries:    0,
		IdleTimeout:       30 * time.Second,
		KeepaliveInterval: 1 * time.Hour,
		Metrics:           rec,
	})

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}
	key := sshpool.PoolKey{TenantID: "tn", KeyID: "kek", Host: srv.Host, Port: srv.Port}

	// 두 conn idle로 만들기.
	for i := 0; i < 2; i++ {
		_, release, err := p.Acquire(context.Background(), key, target)
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		release()
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Close 후 gauge 0.
	_, _, gauge := rec.snapshot()
	if gauge != 0 {
		t.Errorf("idle gauge after Close = %d, want 0", gauge)
	}

	// Close 후 Acquire 거부.
	_, _, err := p.Acquire(context.Background(), key, target)
	if err == nil {
		t.Error("Acquire after Close should fail")
	}
}

func TestPool_IdleReuseMetrics_OnlyDialCountsForMisses(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, nil)

	rec := &recordingPoolMetrics{}
	p := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:      5,
		DialMaxRetries:    0,
		IdleTimeout:       30 * time.Second,
		KeepaliveInterval: 1 * time.Hour,
		Metrics:           rec,
	})
	t.Cleanup(func() { _ = p.Close() })

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}
	key := sshpool.PoolKey{TenantID: "tn", KeyID: "kek", Host: srv.Host, Port: srv.Port}

	// 5번 Acquire/release — 1번 dial + 4번 idle hit 예상.
	for i := 0; i < 5; i++ {
		_, release, err := p.Acquire(context.Background(), key, target)
		if err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
		release()
	}

	dialOK, dialFail, _ := rec.snapshot()
	if dialOK != 1 {
		t.Errorf("dial_total{result=ok} = %d, want 1 (4회는 idle hit)", dialOK)
	}
	if dialFail != 0 {
		t.Errorf("dial_total{result=fail} = %d, want 0", dialFail)
	}
}

func TestPool_IdleReuseRespectsHostLimit(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, nil)

	const limit = 3
	p := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:      limit,
		DialMaxRetries:    0,
		IdleTimeout:       30 * time.Second,
		KeepaliveInterval: 1 * time.Hour,
	})
	t.Cleanup(func() { _ = p.Close() })

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}
	key := sshpool.PoolKey{TenantID: "tn", KeyID: "kek", Host: srv.Host, Port: srv.Port}

	// limit + N 만큼 동시 Acquire — hostLimit semaphore가 직렬화.
	const N = 10
	var (
		wg            sync.WaitGroup
		current       atomic.Int32
		maxConcurrent atomic.Int32
	)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, release, err := p.Acquire(ctx, key, target)
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			defer release()
			cur := current.Add(1)
			for {
				peak := maxConcurrent.Load()
				if cur <= peak || maxConcurrent.CompareAndSwap(peak, cur) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			current.Add(-1)
		}()
	}
	wg.Wait()

	if peak := maxConcurrent.Load(); peak > limit {
		t.Errorf("max concurrent = %d, want ≤ %d (idle 재사용도 limit 존중)", peak, limit)
	}
}
