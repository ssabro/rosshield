package sshpool_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ssabro/rosshield/internal/platform/sshpool"
	"github.com/ssabro/rosshield/internal/platform/sshpool/sshpooltest"
)

// E6.T1 — Pool은 동시 acquire가 PerHostLimit를 절대 초과하지 않음을 보장.
func TestSSHPoolRespectsHostLimit(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, func(cmd string) sshpooltest.ExecResponse {
		// fake가 응답 안 함 — 호출자가 release까지 conn 보유.
		return sshpooltest.ExecResponse{ExitCode: 0}
	})

	const perHostLimit = 3
	pool := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:   perHostLimit,
		DialMaxRetries: 0, // 단순화
	})
	t.Cleanup(func() { _ = pool.Close() })

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}
	key := sshpool.PoolKey{TenantID: "tn_TEST", KeyID: "kek_test", Host: srv.Host, Port: srv.Port}

	const N = 12
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

			client, release, err := pool.Acquire(ctx, key, target)
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			defer release()
			_ = client

			cur := current.Add(1)
			for {
				peak := maxConcurrent.Load()
				if cur <= peak || maxConcurrent.CompareAndSwap(peak, cur) {
					break
				}
			}
			time.Sleep(80 * time.Millisecond) // 보유 시간
			current.Add(-1)
		}()
	}
	wg.Wait()

	peak := maxConcurrent.Load()
	if peak > perHostLimit {
		t.Errorf("max concurrent acquires = %d, want ≤ %d", peak, perHostLimit)
	}
	if peak < 2 { // 적어도 limit 근처까진 도달해야 — 직렬화 검증
		t.Errorf("max concurrent = %d, want ≥ 2 (parallelism not exercised)", peak)
	}
}

// per-tenant limit 강제.
func TestSSHPoolRespectsTenantLimit(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, func(cmd string) sshpooltest.ExecResponse {
		return sshpooltest.ExecResponse{ExitCode: 0}
	})

	pool := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:   100, // tenant limit이 binding constraint
		PerTenantLimit: 2,
		DialMaxRetries: 0,
	})
	t.Cleanup(func() { _ = pool.Close() })

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}
	key := sshpool.PoolKey{TenantID: "tn_T1", KeyID: "k", Host: srv.Host, Port: srv.Port}

	const N = 8
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
			_, release, err := pool.Acquire(ctx, key, target)
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
			time.Sleep(60 * time.Millisecond)
			current.Add(-1)
		}()
	}
	wg.Wait()
	peak := maxConcurrent.Load()
	if peak > 2 {
		t.Errorf("max concurrent = %d, want ≤ 2 (per-tenant limit)", peak)
	}
}

// 두 tenant는 서로 limit 영향 X.
//
// 결정론 보장: sync.Barrier 패턴 — 4 goroutine이 모두 Acquire 완료 후 main이
// release 신호를 줄 때까지 holder 유지. CI runner goroutine scheduling race로
// 일부 goroutine이 acquire 전에 다른 goroutine이 release하던 flaky를 제거.
func TestSSHPoolTenantsIsolated(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, func(cmd string) sshpooltest.ExecResponse {
		return sshpooltest.ExecResponse{ExitCode: 0}
	})

	pool := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:   100,
		PerTenantLimit: 2,
		DialMaxRetries: 0,
	})
	t.Cleanup(func() { _ = pool.Close() })

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}

	// tenant A 2개 + tenant B 2개 동시 보유 → 4개 동시 가능.
	keyA := sshpool.PoolKey{TenantID: "tn_A", KeyID: "kA", Host: srv.Host, Port: srv.Port}
	keyB := sshpool.PoolKey{TenantID: "tn_B", KeyID: "kB", Host: srv.Host, Port: srv.Port}

	var (
		wg            sync.WaitGroup
		current       atomic.Int32
		maxConcurrent atomic.Int32
	)
	const N = 4
	acquired := make(chan struct{}, N) // 각 goroutine acquire 완료 신호
	release := make(chan struct{})     // main → goroutine 동시 release 신호
	mk := func(key sshpool.PoolKey) {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, releaseFn, err := pool.Acquire(ctx, key, target)
		if err != nil {
			t.Errorf("Acquire: %v", err)
			acquired <- struct{}{} // main의 N 카운트가 막히지 않게
			return
		}
		defer releaseFn()
		cur := current.Add(1)
		for {
			peak := maxConcurrent.Load()
			if cur <= peak || maxConcurrent.CompareAndSwap(peak, cur) {
				break
			}
		}
		// 1) Acquire 완료 신호 → main이 모든 N개 확인 후 release 신호 전달
		acquired <- struct{}{}
		// 2) release 신호 대기 — 모든 goroutine이 holder인 상태가 결정론적으로 유지됨
		<-release
		current.Add(-1)
	}
	wg.Add(N)
	go mk(keyA)
	go mk(keyA)
	go mk(keyB)
	go mk(keyB)

	// 모든 N개의 goroutine이 acquire 완료될 때까지 대기 (또는 ctx timeout으로 실패 보고).
	// 이 시점에 maxConcurrent peak이 정확히 N으로 측정됨이 결정론적으로 보장됨.
	for i := 0; i < N; i++ {
		select {
		case <-acquired:
		case <-time.After(10 * time.Second):
			t.Fatalf("timeout waiting for %d goroutines to Acquire (got %d)", N, i)
		}
	}
	close(release) // 모든 goroutine 동시 release
	wg.Wait()

	peak := maxConcurrent.Load()
	if peak < 3 {
		t.Errorf("max concurrent = %d, want ≥ 3 (tenants isolated)", peak)
	}
	if peak > 4 {
		t.Errorf("max concurrent = %d, want ≤ 4", peak)
	}
}

// ctx cancel 시 대기 중 Acquire는 즉시 ctx.Err() 반환.
func TestSSHPoolCancelWaitingAcquire(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, func(cmd string) sshpooltest.ExecResponse { return sshpooltest.ExecResponse{ExitCode: 0} })

	pool := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:   1,
		DialMaxRetries: 0,
	})
	t.Cleanup(func() { _ = pool.Close() })

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}
	key := sshpool.PoolKey{TenantID: "tn", KeyID: "k", Host: srv.Host, Port: srv.Port}

	// 첫 acquire가 슬롯 점유.
	_, release1, err := pool.Acquire(context.Background(), key, target)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer release1()

	// 두 번째 acquire는 대기 → ctx 취소로 즉시 반환.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err = pool.Acquire(ctx, key, target)
	dur := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if dur >= 1*time.Second {
		t.Errorf("Acquire returned after %v, want < 1s (ctx cancel respected)", dur)
	}
}

// Close 후 Acquire는 ErrPoolClosed.
func TestSSHPoolClosedRejectsAcquire(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, func(cmd string) sshpooltest.ExecResponse { return sshpooltest.ExecResponse{} })

	pool := sshpool.NewPool(sshpool.PoolConfig{})
	if err := pool.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}
	key := sshpool.PoolKey{TenantID: "tn", KeyID: "k", Host: srv.Host, Port: srv.Port}

	_, _, err := pool.Acquire(context.Background(), key, target)
	if !errors.Is(err, sshpool.ErrPoolClosed) {
		t.Errorf("err = %v, want ErrPoolClosed", err)
	}
}

// release()는 idempotent — 두 번째 호출 시 no-op.
func TestSSHPoolReleaseIdempotent(t *testing.T) {
	t.Parallel()
	srv := sshpooltest.New(t, func(cmd string) sshpooltest.ExecResponse { return sshpooltest.ExecResponse{} })

	pool := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:   1,
		DialMaxRetries: 0,
	})
	t.Cleanup(func() { _ = pool.Close() })

	target := sshpool.Target{
		Host: srv.Host, Port: srv.Port,
		Username: "u", Auth: dummyAuth(),
		HostKeyCallback: srv.HostKeyCallback(),
	}
	key := sshpool.PoolKey{TenantID: "tn", KeyID: "k", Host: srv.Host, Port: srv.Port}

	_, release, err := pool.Acquire(context.Background(), key, target)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	release()
	release() // 두 번째 호출은 no-op (panic·crash X)

	// 슬롯이 정상 회수됐는지 — 다음 Acquire 즉시 성공.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, release2, err := pool.Acquire(ctx, key, target)
	if err != nil {
		t.Fatalf("Acquire after double-release: %v", err)
	}
	defer release2()
}

// dial이 항상 실패하면 backoff retry 후 ErrPoolDialFailed.
func TestSSHPoolDialBackoffRetries(t *testing.T) {
	t.Parallel()

	pool := sshpool.NewPool(sshpool.PoolConfig{
		PerHostLimit:   1,
		DialMaxRetries: 2,
		DialBaseDelay:  10 * time.Millisecond,
	})
	t.Cleanup(func() { _ = pool.Close() })

	// listen하지 않는 포트 → connection refused.
	target := sshpool.Target{
		Host:            "127.0.0.1",
		Port:            1, // privileged·unused
		Username:        "u",
		Auth:            dummyAuth(),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	key := sshpool.PoolKey{TenantID: "tn", KeyID: "k", Host: target.Host, Port: target.Port}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, _, err := pool.Acquire(ctx, key, target)
	dur := time.Since(start)

	if err == nil {
		t.Fatal("expected dial failure, got nil")
	}
	// 1 + 2 = 3 attempts, between attempts 2 sleeps (base*1 + jitter, base*2 + jitter)
	// 최소 base = 10ms, 시간 너무 짧으면 재시도 안 한 것.
	if dur < 15*time.Millisecond {
		t.Errorf("Acquire returned in %v, want ≥ 15ms (backoff)", dur)
	}
}
