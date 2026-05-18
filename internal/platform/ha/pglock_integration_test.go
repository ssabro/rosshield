//go:build integration

// pglock_integration_test.go — E25 Stage 4 PGLock 통합 테스트 (testcontainers-go).
//
// 본 파일은 `-tags=integration` 빌드 태그가 붙어야 컴파일됩니다.
// docker daemon 부재 시 testcontainers-go가 immediate fail — t.Skip 가드.
//
// 실행:
//
//	go test -tags=integration -count=1 ./internal/platform/ha/
//
// 검증 항목 (design doc §6 E25.T1·T2·T4 + 단조 epoch):
//
//   - TestPGLockSingleHolderConcurrent: 두 PGLock 인스턴스 동시 TryAcquire → 정확히 1개 성공.
//   - TestPGLockReleasedOnSessionDrop: 첫 PGLock Release → 두 번째 PGLock TryAcquire 성공 + epoch 증가.
//   - TestEpochMonotonicAcrossAcquisitions: 5회 Acquire/Release 반복 → epoch 1~5 단조 증가.
//   - TestPGLockHeartbeatPingsLiveConn: Heartbeat 성공 → Release 후 Heartbeat → "not held" 에러.
//
// 헬퍼 재사용 메모: storage/postgres/integration_test.go의 newPGFixture는 tenant 도메인 import가
// 묶여있어 ha 패키지에서 재사용하면 도메인 경계(원칙 §05)를 위반합니다. 따라서 본 파일은
// pgxpool + golang-migrate를 직접 부팅하는 자체 헬퍼(setupHAFixture)를 사용합니다 — postgres
// 어댑터를 import하지 않아 ha 패키지 의존 그래프를 최소화합니다.

package ha_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ssabro/rosshield/internal/platform/ha"
	pgstorage "github.com/ssabro/rosshield/internal/platform/storage/postgres"
)

// 본 테스트 전용 advisory lock id — 운영 코드와 별 namespace.
// E25 운영 lockID는 bootstrap에서 별 상수로 관리; 통합 테스트는 격리된 임의값.
const testLockID int64 = 0x726f7373686c6431 // "rosshld1"

// haFixture는 단일 테스트당 격리된 PG 컨테이너 + pgxpool입니다.
type haFixture struct {
	pool *pgxpool.Pool
}

// setupHAFixture는 PG 16 컨테이너를 부팅하고 0001~0023 마이그레이션을 적용한 후
// pgxpool을 반환합니다. docker 없을 시 t.Skip.
//
// 자체 헬퍼인 이유: storage/postgres/integration_test.go의 newPGFixture는 tenant 도메인
// 시드(create result)을 함께 반환하기 위해 도메인 패키지를 import합니다. ha 패키지는
// 도메인 무지(원칙 §05)를 유지해야 하므로 별 헬퍼로 분리합니다.
func setupHAFixture(t *testing.T) *haFixture {
	t.Helper()
	ctx := context.Background()

	pgC, err := tcpg.Run(ctx, "postgres:16-alpine",
		tcpg.WithDatabase("rosshield_ha_test"),
		tcpg.WithUsername("test"),
		tcpg.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			// CI fix cascade 12회차: startup timeout 30s → 90s.
			// 4 test 동시 PG container spin up + image pull(첫 회) 시 30s 초과 가능.
			// 초과 시 t.Skipf로 끝나면 그나마 다행이지만, container start 자체가 hang
			// (docker daemon backpressure)이면 testcontainers 내부에서 추가 대기.
			// 90s 여유 확보로 CI runner 부하 흡수.
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		t.Skipf("docker unavailable or PG container failed: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = pgC.Terminate(shutdownCtx)
	})

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	// MinConns=2: 두 PGLock이 같은 pool을 공유할 때 long-hold conn + 두 번째 시도용.
	cfg.MinConns = 2
	cfg.MaxConns = 8

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig: %v", err)
	}
	t.Cleanup(pool.Close)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		t.Fatalf("pool ping: %v", err)
	}

	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer migrateCancel()
	if err := applyMigrations(migrateCtx, pool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	return &haFixture{pool: pool}
}

// applyMigrations은 postgres 어댑터가 embed한 migration 0001~0030을 적용합니다.
// pgstorage.MigrationsFS 만 가져와 사용 — 어댑터의 *Postgres 인스턴스는 만들지 않음.
//
// ctx는 60s timeout 권장 — migrate Up()이 internal advisory lock + 30개 migration
// 직렬 적용으로 시간 소요. CI 부하 시 무한 대기 차단.
func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	src, err := iofs.New(pgstorage.MigrationsFS, "migrations")
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()

	dbDrv, err := migratepg.WithInstance(sqlDB, &migratepg.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", dbDrv)
	if err != nil {
		return err
	}
	// migrate-go는 자체 컨텍스트 미수신 — ctx 통한 cancel은 best-effort.
	// 별도 goroutine에서 Up() 호출 + ctx done 시 graceful close.
	done := make(chan error, 1)
	go func() { done <- m.Up() }()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return err
		}
		return nil
	case <-ctx.Done():
		// migrate가 hang하면 sqlDB.Close()로 강제 종료. defer가 처리.
		return fmt.Errorf("apply migrations: %w", ctx.Err())
	}
}

// === TestPGLockSingleHolderConcurrent ===
//
// 두 PGLock 인스턴스가 같은 pool/lockID로 동시 TryAcquire → 정확히 1개만 성공.
// design doc §6 E25.T1.
func TestPGLockSingleHolderConcurrent(t *testing.T) {
	// CI fix cascade 12회차: t.Parallel() 제거 — 4 test 동시 PG container 부하 회피.
	// 각 test는 자체 testcontainer 보유라 동시 4 PG image pull/start가 GitHub Actions
	// runner의 docker daemon backpressure 유발 가설.
	fx := setupHAFixture(t)
	// 각 PG 호출은 30s timeout — 본체 hang(advisory lock 대기·tx commit hang 등) 차단.
	// 정상 path는 sub-second에 끝남.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lockA := ha.NewPGLock(fx.pool, testLockID)
	lockB := ha.NewPGLock(fx.pool, testLockID)
	t.Cleanup(func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer releaseCancel()
		_ = lockA.Release(releaseCtx)
		_ = lockB.Release(releaseCtx)
	})

	type result struct {
		ok    bool
		epoch int64
		err   error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ok, epoch, err := lockA.TryAcquire(ctx, "host-a:1111")
		results <- result{ok, epoch, err}
	}()
	go func() {
		defer wg.Done()
		ok, epoch, err := lockB.TryAcquire(ctx, "host-b:2222")
		results <- result{ok, epoch, err}
	}()
	wg.Wait()
	close(results)

	winners := 0
	losers := 0
	var winnerEpoch int64
	for r := range results {
		if r.err != nil {
			t.Fatalf("TryAcquire err: %v", r.err)
		}
		if r.ok {
			winners++
			winnerEpoch = r.epoch
			if r.epoch != 1 {
				t.Errorf("winner epoch = %d, want 1 (first acquisition)", r.epoch)
			}
		} else {
			losers++
			if r.epoch != 0 {
				t.Errorf("loser epoch = %d, want 0", r.epoch)
			}
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("winners=%d losers=%d, want 1/1", winners, losers)
	}

	// leader_epoch 테이블 검증: 1 row, current=1, epoch=winnerEpoch.
	rows, err := fx.pool.Query(ctx, "SELECT epoch, current FROM leader_epoch")
	if err != nil {
		t.Fatalf("query leader_epoch: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var epoch int64
		var current int16
		if err := rows.Scan(&epoch, &current); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if epoch != winnerEpoch {
			t.Errorf("leader_epoch.epoch = %d, want %d", epoch, winnerEpoch)
		}
		if current != 1 {
			t.Errorf("leader_epoch.current = %d, want 1", current)
		}
	}
	if count != 1 {
		t.Errorf("leader_epoch row count = %d, want 1", count)
	}
}

// === TestPGLockReleasedOnSessionDrop ===
//
// 첫 PGLock TryAcquire 성공 → Release(세션 정리) → 두 번째 PGLock 동일 lockID로 TryAcquire 성공.
// epoch가 단조 증가 + leader_epoch 테이블에 2 row(이전 current=0, 새 current=1).
// design doc §6 E25.T2 (failover).
func TestPGLockReleasedOnSessionDrop(t *testing.T) {
	// CI fix cascade 12회차: t.Parallel() 제거 + ctx timeout 추가.
	fx := setupHAFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lockA := ha.NewPGLock(fx.pool, testLockID)
	ok, epochA, err := lockA.TryAcquire(ctx, "host-a:1111")
	if err != nil {
		t.Fatalf("lockA TryAcquire: %v", err)
	}
	if !ok {
		t.Fatalf("lockA expected to acquire on fresh PG")
	}
	if epochA != 1 {
		t.Errorf("epochA = %d, want 1", epochA)
	}

	// Release simulates leader stepping down (PG advisory_unlock + conn 반환).
	// 세션 강제 종료(pg_terminate_backend)와 동등한 효과 — lock 해제됨.
	if err := lockA.Release(ctx); err != nil {
		t.Fatalf("lockA Release: %v", err)
	}

	// 두 번째 PGLock이 같은 lockID 재획득.
	lockB := ha.NewPGLock(fx.pool, testLockID)
	t.Cleanup(func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer releaseCancel()
		_ = lockB.Release(releaseCtx)
	})
	ok, epochB, err := lockB.TryAcquire(ctx, "host-b:2222")
	if err != nil {
		t.Fatalf("lockB TryAcquire: %v", err)
	}
	if !ok {
		t.Fatalf("lockB expected to acquire after lockA released")
	}
	if epochB <= epochA {
		t.Errorf("epochB = %d must be > epochA %d (monotonic)", epochB, epochA)
	}
	if epochB != 2 {
		t.Errorf("epochB = %d, want 2", epochB)
	}

	// leader_epoch 테이블: 2 row, epoch=1은 current=0, epoch=2는 current=1.
	rows, err := fx.pool.Query(ctx, "SELECT epoch, current FROM leader_epoch ORDER BY epoch ASC")
	if err != nil {
		t.Fatalf("query leader_epoch: %v", err)
	}
	defer rows.Close()
	type row struct {
		epoch   int64
		current int16
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.epoch, &r.current); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("leader_epoch rows = %d, want 2", len(got))
	}
	if got[0].epoch != 1 || got[0].current != 0 {
		t.Errorf("row[0] = %+v, want {epoch:1, current:0}", got[0])
	}
	if got[1].epoch != 2 || got[1].current != 1 {
		t.Errorf("row[1] = %+v, want {epoch:2, current:1}", got[1])
	}
}

// === TestEpochMonotonicAcrossAcquisitions ===
//
// Acquire→Release를 5회 반복 → epoch 1, 2, 3, 4, 5 (단조 증가).
// leader_epoch 5 row, 마지막만 current=1.
// design doc §6 "단조 epoch".
func TestEpochMonotonicAcrossAcquisitions(t *testing.T) {
	// CI fix cascade 12회차: t.Parallel() 제거 + 각 round별 ctx timeout + log.
	// 이전 hang(8m timeout)이 어느 round에서 멈췄는지 다음 CI run에서 식별 가능.
	fx := setupHAFixture(t)

	const rounds = 5
	gotEpochs := make([]int64, 0, rounds)
	for i := 0; i < rounds; i++ {
		// 매 round 자체 ctx timeout 15s — TryAcquire + Release가 각각 sub-second 정상.
		// hang하면 round i와 단계(acquire vs release)가 fail msg에 명시.
		roundCtx, roundCancel := context.WithTimeout(context.Background(), 15*time.Second)
		t.Logf("round %d: NewPGLock + TryAcquire", i)
		lock := ha.NewPGLock(fx.pool, testLockID)
		ok, epoch, err := lock.TryAcquire(roundCtx, "host-cycle")
		if err != nil {
			roundCancel()
			t.Fatalf("round %d TryAcquire: %v", i, err)
		}
		if !ok {
			roundCancel()
			t.Fatalf("round %d expected to acquire", i)
		}
		t.Logf("round %d: acquired epoch=%d, Release", i, epoch)
		gotEpochs = append(gotEpochs, epoch)
		if err := lock.Release(roundCtx); err != nil {
			roundCancel()
			t.Fatalf("round %d Release: %v", i, err)
		}
		roundCancel()
		t.Logf("round %d: released", i)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 단조 증가 검증.
	for i, e := range gotEpochs {
		want := int64(i + 1)
		if e != want {
			t.Errorf("epoch[%d] = %d, want %d", i, e, want)
		}
	}

	// leader_epoch: 5 row, 마지막(epoch=5)만 current=1.
	rows, err := fx.pool.Query(ctx, "SELECT epoch, current FROM leader_epoch ORDER BY epoch ASC")
	if err != nil {
		t.Fatalf("query leader_epoch: %v", err)
	}
	defer rows.Close()
	count := 0
	currentCount := 0
	var lastEpochCurrent int64
	for rows.Next() {
		count++
		var epoch int64
		var current int16
		if err := rows.Scan(&epoch, &current); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if current == 1 {
			currentCount++
			lastEpochCurrent = epoch
		}
	}
	if count != rounds {
		t.Errorf("leader_epoch rows = %d, want %d", count, rounds)
	}
	if currentCount != 1 {
		t.Errorf("current=1 row count = %d, want 1", currentCount)
	}
	if lastEpochCurrent != int64(rounds) {
		t.Errorf("current epoch = %d, want %d", lastEpochCurrent, rounds)
	}
}

// === TestPGLockHeartbeatPingsLiveConn ===
//
// TryAcquire 성공 → Heartbeat() = nil → Release() → Heartbeat() = "not held" 에러.
// design doc §6 E25.T4.
func TestPGLockHeartbeatPingsLiveConn(t *testing.T) {
	// CI fix cascade 12회차: t.Parallel() 제거 + 단계별 ctx timeout + log.
	fx := setupHAFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lock := ha.NewPGLock(fx.pool, testLockID)
	t.Cleanup(func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer releaseCancel()
		_ = lock.Release(releaseCtx)
	})

	t.Logf("step 1: TryAcquire")
	ok, _, err := lock.TryAcquire(ctx, "host-hb")
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if !ok {
		t.Fatalf("expected acquire success")
	}

	t.Logf("step 2: Heartbeat while held")
	// 보유 중 — Heartbeat 정상.
	if err := lock.Heartbeat(ctx); err != nil {
		t.Fatalf("Heartbeat while held: %v", err)
	}

	t.Logf("step 3: Release")
	// Release 후 Heartbeat → 보유 conn 없음 → 에러.
	if err := lock.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
	t.Logf("step 4: Heartbeat after Release (expect not held)")
	err = lock.Heartbeat(ctx)
	if err == nil {
		t.Fatalf("Heartbeat after Release expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not held") {
		t.Errorf("Heartbeat err = %v, want substring \"not held\"", err)
	}
}
