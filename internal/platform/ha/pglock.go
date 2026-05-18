package ha

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGLock은 PostgreSQL advisory lock 기반 Lock 구현입니다.
//
// 동작 모델:
//   - TryAcquire 성공 시 dedicated *pgxpool.Conn을 long-hold (release 전까지).
//     PG 세션 살아있음 = lock 살아있음. 세션 끊기면 PG가 자동 release.
//   - epoch는 PG sequence로 발급(`nextval('leader_epoch_seq')`) — 단조 증가 보장.
//   - leader_epoch 테이블에 (epoch, leader_id, acquired_at, current=1) 기록.
//     기존 current=1 row는 0으로 다운그레이드. partial unique index가 동시성 보장.
//   - Heartbeat은 보유 conn에 SELECT 1 ping. 실패 시 leader 자격 상실.
//
// 권장: 본 lock 전용 *pgxpool.Pool을 분리(maxConns=2 등)해 write traffic과 격리.
// 다만 Phase 5 첫 stage에서는 main pool을 공유 — 향후 stage에서 분리.
type PGLock struct {
	pool   *pgxpool.Pool
	lockID int64

	mu   sync.Mutex
	conn *pgxpool.Conn // 보유 중일 때만 non-nil
}

// NewPGLock은 PG advisory lock 어댑터를 생성합니다.
func NewPGLock(pool *pgxpool.Pool, lockID int64) *PGLock {
	return &PGLock{pool: pool, lockID: lockID}
}

// 컴파일 시점 인터페이스 매칭 보증.
var _ Lock = (*PGLock)(nil)

// TryAcquire는 advisory lock을 한 번 시도합니다.
// 성공 시 dedicated conn을 보유 + epoch 발급 + leader_epoch 테이블 기록.
func (l *PGLock) TryAcquire(ctx context.Context, leaderID string) (bool, int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn != nil {
		// 이미 보유 중 — TryAcquire는 idempotent로 false 리턴 (재시도 의미 없음).
		// 대신 호출자가 IsHeld 체크 책임. 본 함수는 새 시도 시에만 호출됨.
		return false, 0, errors.New("pglock: already held")
	}

	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return false, 0, fmt.Errorf("pglock: acquire conn: %w", err)
	}

	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", l.lockID).Scan(&got); err != nil {
		conn.Release()
		return false, 0, fmt.Errorf("pglock: try_advisory_lock: %w", err)
	}
	if !got {
		// 다른 인스턴스가 보유 중. conn 반납.
		conn.Release()
		return false, 0, nil
	}

	// epoch 발급 — sequence는 PG가 atomic하게 단조 증가 보장.
	var epoch int64
	if err := conn.QueryRow(ctx, "SELECT nextval('leader_epoch_seq')").Scan(&epoch); err != nil {
		// epoch 발급 실패 — lock 즉시 해제.
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", l.lockID)
		conn.Release()
		return false, 0, fmt.Errorf("pglock: epoch nextval: %w", err)
	}

	// leader_epoch 테이블 update (current 마킹). 트랜잭션 안에서 atomic하게.
	if err := l.markCurrentEpoch(ctx, conn, epoch, leaderID); err != nil {
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", l.lockID)
		conn.Release()
		return false, 0, fmt.Errorf("pglock: mark current epoch: %w", err)
	}

	l.conn = conn
	return true, epoch, nil
}

// markCurrentEpoch는 leader_epoch 테이블에 새 epoch row를 INSERT하고
// 기존 current=1 row를 0으로 다운그레이드합니다.
//
// partial unique index `WHERE current=1`이 둘 이상의 current row를 거부하므로
// race가 있어도 한쪽만 성공.
func (l *PGLock) markCurrentEpoch(ctx context.Context, conn *pgxpool.Conn, epoch int64, leaderID string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "UPDATE leader_epoch SET current = 0 WHERE current = 1"); err != nil {
		return fmt.Errorf("downgrade prev: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(ctx,
		"INSERT INTO leader_epoch (epoch, leader_id, acquired_at, current) VALUES ($1, $2, $3, 1)",
		epoch, leaderID, now,
	); err != nil {
		return fmt.Errorf("insert current: %w", err)
	}

	return tx.Commit(ctx)
}

// Heartbeat은 보유 중인 conn에 SELECT 1 ping을 보냅니다.
// conn이 끊어졌으면 에러 — Manager가 demote 처리.
//
// 동시성: mu를 query 동안 잡고 있어 동시 Release()와 race 없음.
// Release는 mu.Lock → l.conn = nil → mu.Unlock 후 conn.Release() 호출이라
// Heartbeat이 mu 보유 중이면 Release는 대기. SELECT 1은 sub-ms라 blocking
// window 무시 가능. CI fix cascade 12회차에서 race window 보완(이전 mu 풀고
// conn 사용 패턴은 Release 직후 conn.QueryRow가 released pool conn에 query를
// 날려 무한 hang 가능성 있었음).
func (l *PGLock) Heartbeat(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn == nil {
		return errors.New("pglock: not held")
	}
	var one int
	if err := l.conn.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("pglock: heartbeat: %w", err)
	}
	return nil
}

// Release는 advisory lock을 해제하고 conn을 반환합니다.
// 멱등 — 보유 중이 아니면 nil.
//
// 호출자가 timeout 없는 ctx를 넘기면 PG hang 시 무한 대기. CI fix cascade
// 12회차에서 내부 두 Exec에 10s timeout fallback 추가 — ctx 자체에 deadline이
// 있으면 그것을 사용, 없으면 10s timeout 생성. 결정론적 종료 보장.
func (l *PGLock) Release(ctx context.Context) error {
	l.mu.Lock()
	conn := l.conn
	l.conn = nil
	l.mu.Unlock()

	if conn == nil {
		return nil
	}
	defer conn.Release()

	execCtx, cancel := withFallbackTimeout(ctx, 10*time.Second)
	defer cancel()

	// best-effort — 어차피 conn release 시 PG가 자동 unlock.
	if _, err := conn.Exec(execCtx, "SELECT pg_advisory_unlock($1)", l.lockID); err != nil {
		return fmt.Errorf("pglock: advisory_unlock: %w", err)
	}
	// current epoch 다운그레이드 (best-effort, 새 leader가 어차피 덮어씀).
	_, _ = conn.Exec(execCtx, "UPDATE leader_epoch SET current = 0 WHERE current = 1")
	return nil
}

// withFallbackTimeout은 ctx에 deadline이 없으면 fallback timeout을 적용합니다.
// ctx에 이미 deadline이 있으면 그대로 사용 (cancel은 noop).
func withFallbackTimeout(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, fallback)
}
