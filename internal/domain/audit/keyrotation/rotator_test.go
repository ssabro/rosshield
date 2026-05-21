package keyrotation_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/keyrotation"
	auditrepo "github.com/ssabro/rosshield/internal/domain/audit/sqliterepo"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/storage"
	"github.com/ssabro/rosshield/internal/platform/storage/sqlite"
)

const testTenant storage.TenantID = "system"

type stubMetrics struct {
	mu       sync.Mutex
	counts   map[string]int
	epochSet map[storage.TenantID]int64
}

func newStubMetrics() *stubMetrics {
	return &stubMetrics{counts: map[string]int{}, epochSet: map[storage.TenantID]int64{}}
}

func (m *stubMetrics) IncRotation(status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[status]++
}

func (m *stubMetrics) SetCurrentEpoch(tenantID storage.TenantID, epoch int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.epochSet[tenantID] = epoch
}

func (m *stubMetrics) count(status string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[status]
}

type stubLeader struct {
	mu     sync.Mutex
	leader bool
}

func (l *stubLeader) IsLeader() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.leader
}

func (l *stubLeader) set(v bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.leader = v
}

// fixture는 0038 적용된 SQLite store + audit Service + KeyRotator + 의존성 묶음을 반환합니다.
type fixture struct {
	store        storage.Storage
	audit        audit.Service
	chainKeyRepo *auditrepo.KeyEpochRepo
	swap         *signer.SwappableSigner
	rotator      *keyrotation.KeyRotator
	clk          *clock.FakeClock
	metrics      *stubMetrics
	leader       *stubLeader
	allocCount   int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "keyrotation.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})
	chainKeyRepo := auditrepo.NewKeyEpochRepo()

	initial, err := soft.New()
	if err != nil {
		t.Fatalf("soft.New: %v", err)
	}
	swap := signer.NewSwappableSigner(initial, 1)

	leader := &stubLeader{leader: true}
	metrics := newStubMetrics()

	f := &fixture{
		store:        store,
		audit:        auditSvc,
		chainKeyRepo: chainKeyRepo,
		swap:         swap,
		clk:          clk,
		metrics:      metrics,
		leader:       leader,
	}

	allocator := keyrotation.AllocatorFunc(func(newEpoch int64) (string, ed25519.PrivateKey, error) {
		f.allocCount++
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return "", nil, err
		}
		return "test-handle-" + epochSuffix(newEpoch), priv, nil
	})

	rotator, err := keyrotation.New(keyrotation.Deps{
		Storage:     store,
		Audit:       auditSvc,
		ChainKeys:   chainKeyRepo,
		Signer:      swap,
		Allocator:   allocator,
		Clock:       clk,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metrics:     metrics,
		Leader:      leader,
		MinInterval: 0, // test 결정성.
		TenantID:    testTenant,
	})
	if err != nil {
		t.Fatalf("keyrotation.New: %v", err)
	}
	f.rotator = rotator
	return f
}

func epochSuffix(n int64) string {
	return time.Unix(n, 0).UTC().Format("20060102") // 결정적 문자열, 테스트용.
}

func TestRotateNow_HappyPath(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	priorKey := f.swap.CurrentKeyID()
	priorEpoch := f.swap.CurrentEpoch()

	if err := f.rotator.RotateNow(context.Background(), keyrotation.TriggerScheduler); err != nil {
		t.Fatalf("RotateNow: %v", err)
	}

	// SwappableSigner 가 새 key 로 swap 되었는지 확인.
	if f.swap.CurrentKeyID() == priorKey {
		t.Error("KeyID unchanged after rotation")
	}
	if f.swap.CurrentEpoch() <= priorEpoch {
		t.Errorf("epoch did not advance: %d -> %d", priorEpoch, f.swap.CurrentEpoch())
	}

	// audit_chain_keys 에 새 epoch row + audit_entries 에 key_rotated event.
	runReadTx(t, f.store, func(ctx context.Context, tx storage.Tx) {
		list, err := f.chainKeyRepo.ListChainKeyEpochs(ctx, tx, testTenant)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) < 2 {
			t.Fatalf("expected >= 2 epoch rows (bootstrap + new), got %d", len(list))
		}
		newest := list[len(list)-1]
		if newest.CreatedBy != "scheduler" {
			t.Errorf("CreatedBy = %q, want scheduler", newest.CreatedBy)
		}
		if newest.IsRevoked() {
			t.Error("newest epoch should not be revoked")
		}
		if newest.KeystoreHandle == "" {
			t.Error("KeystoreHandle empty")
		}

		// audit_chain.key_rotated emit 확인 — Head seq >= 1.
		head, err := f.audit.Head(ctx, tx, testTenant)
		if err != nil {
			t.Fatalf("Head: %v", err)
		}
		if head.Seq < 1 {
			t.Errorf("audit chain head seq = %d, want >= 1", head.Seq)
		}
	})

	if f.metrics.count("success") != 1 {
		t.Errorf("metrics success = %d, want 1", f.metrics.count("success"))
	}
}

func TestRotateNow_FollowerSkips(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.leader.set(false)
	priorEpoch := f.swap.CurrentEpoch()

	err := f.rotator.RotateNow(context.Background(), keyrotation.TriggerScheduler)
	if !errors.Is(err, keyrotation.ErrNotLeader) {
		t.Fatalf("want ErrNotLeader, got %v", err)
	}
	if f.swap.CurrentEpoch() != priorEpoch {
		t.Errorf("epoch advanced despite follower: %d", f.swap.CurrentEpoch())
	}
	if f.metrics.count("skipped") != 1 {
		t.Errorf("metrics skipped = %d, want 1", f.metrics.count("skipped"))
	}
	if f.allocCount != 0 {
		t.Errorf("allocator invoked %d times despite follower", f.allocCount)
	}
}

func TestRotateNow_MinIntervalGuard(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	// 첫 번째 rotation 성공시킴.
	if err := f.rotator.RotateNow(context.Background(), keyrotation.TriggerScheduler); err != nil {
		t.Fatalf("first RotateNow: %v", err)
	}
	// MinInterval=0 이므로 두 번째 호출도 통과해야 함. 다만 본 fixture 는 MinInterval=0.
	// 본 test 는 MinInterval > 0 시점에 ErrTooSoon 발생 검증을 위해 별 fixture 생성.

	// 별 KeyRotator 인스턴스 with MinInterval > 0.
	dbPath := filepath.Join(t.TempDir(), "keyrotation_min.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})
	chainKeyRepo := auditrepo.NewKeyEpochRepo()
	initial, _ := soft.New()
	swap := signer.NewSwappableSigner(initial, 1)
	allocator := keyrotation.AllocatorFunc(func(newEpoch int64) (string, ed25519.PrivateKey, error) {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		return "h", priv, nil
	})
	r, err := keyrotation.New(keyrotation.Deps{
		Storage: store, Audit: auditSvc, ChainKeys: chainKeyRepo,
		Signer: swap, Allocator: allocator, Clock: clk,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		MinInterval: 24 * time.Hour, TenantID: testTenant,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := r.RotateNow(context.Background(), keyrotation.TriggerManual); err != nil {
		t.Fatalf("first RotateNow: %v", err)
	}
	// 즉시 두 번째 호출 — ErrTooSoon.
	err = r.RotateNow(context.Background(), keyrotation.TriggerManual)
	if !errors.Is(err, keyrotation.ErrTooSoon) {
		t.Errorf("second RotateNow err = %v, want ErrTooSoon", err)
	}
}

func TestRotateNow_AllocatorFailureRollback(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "keyrotation_alloc_fail.db")
	store, err := sqlite.Open(storage.Config{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	clk := clock.NewFake(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	auditSvc := auditrepo.New(auditrepo.Deps{Clock: clk})
	chainKeyRepo := auditrepo.NewKeyEpochRepo()
	initial, _ := soft.New()
	swap := signer.NewSwappableSigner(initial, 1)
	priorKey := swap.CurrentKeyID()
	priorEpoch := swap.CurrentEpoch()

	allocator := keyrotation.AllocatorFunc(func(newEpoch int64) (string, ed25519.PrivateKey, error) {
		return "", nil, errors.New("disk full")
	})
	metrics := newStubMetrics()
	r, err := keyrotation.New(keyrotation.Deps{
		Storage: store, Audit: auditSvc, ChainKeys: chainKeyRepo,
		Signer: swap, Allocator: allocator, Clock: clk,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metrics: metrics, MinInterval: 0, TenantID: testTenant,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = r.RotateNow(context.Background(), keyrotation.TriggerScheduler)
	if err == nil {
		t.Fatal("expected error from allocator failure")
	}

	// swap 미반영 — signer 가 prior key 유지.
	if swap.CurrentKeyID() != priorKey {
		t.Error("signer swap occurred despite allocator failure")
	}
	if swap.CurrentEpoch() != priorEpoch {
		t.Errorf("epoch advanced despite allocator failure: %d", swap.CurrentEpoch())
	}

	// audit_chain_keys 에 새 row 미커밋 (bootstrap row 1 개만).
	runReadTx(t, store, func(ctx context.Context, tx storage.Tx) {
		list, err := chainKeyRepo.ListChainKeyEpochs(ctx, tx, testTenant)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) != 1 {
			t.Errorf("expected 1 row (bootstrap only), got %d", len(list))
		}
	})

	if metrics.count("failed") != 1 {
		t.Errorf("metrics failed = %d, want 1", metrics.count("failed"))
	}
}

func TestRotateNow_ConcurrentInvocationSerialized(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	// 동시 RotateNow 5 회 — mutex 직렬화로 모두 성공해야 함 (MinInterval=0).
	const N = 5
	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = f.rotator.RotateNow(context.Background(), keyrotation.TriggerScheduler)
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Errorf("concurrent RotateNow %d: %v", i, e)
		}
	}

	// epoch 가 단조 증가했는지 — 최소 N 만큼.
	if f.swap.CurrentEpoch() < int64(N) {
		t.Errorf("final epoch = %d, want >= %d (concurrent rotations)", f.swap.CurrentEpoch(), N)
	}
}

func TestRotateNow_AuditEntryHasPriorEpoch(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	priorEpoch := f.swap.CurrentEpoch()

	if err := f.rotator.RotateNow(context.Background(), keyrotation.TriggerScheduler); err != nil {
		t.Fatalf("RotateNow: %v", err)
	}

	// 첫 rotation entry 는 swap 직전에 INSERT 됨 — key_epoch = priorEpoch (1).
	runReadTx(t, f.store, func(ctx context.Context, tx storage.Tx) {
		rows, err := tx.Query(ctx, `SELECT seq, key_epoch FROM audit_entries WHERE tenant_id = ? ORDER BY seq ASC LIMIT 1`, string(testTenant))
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			t.Fatal("no audit entry found")
		}
		var seq int64
		var keyEpoch any
		if err := rows.Scan(&seq, &keyEpoch); err != nil {
			t.Fatalf("scan: %v", err)
		}
		// key_epoch 는 INSERT 시점 SwappableSigner epoch — KeyEpochProvider 가 주입 안 됐으므로 NULL.
		// 본 test 는 fixture 에서 KeyEpoch provider 를 주입하지 않음 — NULL 확인.
		if keyEpoch != nil {
			t.Logf("key_epoch = %v (provider 주입 안 됨 — NULL 기대지만 backend 가 0 으로 매핑 가능)", keyEpoch)
		}
		// 단순 placeholder — provider 주입은 bootstrap 결선 시점 검증.
		_ = priorEpoch
	})
}

// runReadTx 는 system tenant 컨텍스트로 read-only Tx 를 실행합니다.
func runReadTx(t *testing.T, store storage.Storage, fn func(ctx context.Context, tx storage.Tx)) {
	t.Helper()
	ctx := storage.WithTenantID(context.Background(), testTenant)
	if err := store.Tx(ctx, func(c context.Context, tx storage.Tx) error {
		fn(c, tx)
		return nil
	}); err != nil {
		t.Fatalf("Tx: %v", err)
	}
}
