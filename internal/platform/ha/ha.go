// Package ha implements E25 leader-election + leader/follower role management
// for high-availability deployments.
//
// 본 패키지는 PostgreSQL advisory lock(`pg_try_advisory_lock`) 위에 5초 주기
// heartbeat을 두어 단일 leader 보장 + 자동 failover를 제공합니다. fence token
// (leader epoch)으로 GC pause·split-brain을 방어합니다.
//
// 설계: docs/design/notes/e25-ha-design.md (R30-2 = PG advisory lock + leader/
// follower + 고정 lock_id + sqlite 부팅 거부, 2026-05-11 결정).
//
// 비목표:
//   - sqlite 환경 — advisory lock 동등 기능 부재로 HA 비대상.
//   - PG 자체 HA (별도 streaming replication 가정).
//   - active-active.
//
// 도메인 import 가드: 본 패키지는 internal/domain/* 를 import하지 않습니다.
// 도메인 측에서 leader 상태가 필요하면 RoleProvider 인터페이스를 통한 어댑터
// 주입(원칙 §05 도메인 경계 준수).
package ha

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Role은 인스턴스의 현재 역할입니다.
type Role int32

const (
	// RoleFollower는 leader 자격을 보유하지 않은 상태입니다.
	// audit chain INSERT·write API·스케줄러가 비활성.
	RoleFollower Role = 0

	// RoleLeader는 PG advisory lock을 보유한 단일 인스턴스입니다.
	// audit chain INSERT·write API·스케줄러가 활성.
	RoleLeader Role = 1
)

// String은 운영자 친화 문자열입니다 (/healthz·로그용).
func (r Role) String() string {
	switch r {
	case RoleLeader:
		return "leader"
	case RoleFollower:
		return "follower"
	default:
		return "unknown"
	}
}

// ErrNotLeader는 follower 인스턴스가 leader-only 작업을 시도할 때 반환됩니다.
// API 미들웨어는 이를 503 Service Unavailable + NOT_LEADER 코드로 매핑합니다.
var ErrNotLeader = errors.New("ha: instance is not leader")

// State는 Manager가 외부에 노출하는 현재 상태 스냅샷입니다.
//
// /healthz 응답 + Prometheus 메트릭 + 운영 로그가 본 구조체를 사용합니다.
type State struct {
	Enabled         bool
	Role            Role
	Epoch           int64
	LeaderID        string
	LastHeartbeatAt time.Time
}

// RoleProvider는 도메인 코드(audit·scan·scheduler)가 leader 여부를 질의할 수
// 있는 minimal interface입니다.
//
// 도메인 패키지는 본 인터페이스만 의존 — Manager 구체 타입 import 금지.
// bootstrap이 *Manager를 RoleProvider로 주입합니다.
type RoleProvider interface {
	IsLeader() bool
	CurrentEpoch() int64
}

// Lock은 PG advisory lock 어댑터의 minimal interface입니다.
//
// 본 인터페이스는 Manager가 PG에 직접 의존하지 않게 하기 위한 추상화입니다.
// 실제 구현은 internal/platform/ha/pglock.go에 있고, 단위 테스트는 본 인터페이스를
// mock으로 대체합니다.
type Lock interface {
	// TryAcquire는 advisory lock을 한 번 시도합니다.
	// 성공 시 (true, epoch, nil), 다른 인스턴스가 보유 중이면 (false, 0, nil),
	// 실제 에러(connection 끊김 등)는 (false, 0, err).
	TryAcquire(ctx context.Context, leaderID string) (acquired bool, epoch int64, err error)

	// Heartbeat는 보유 중인 advisory lock의 conn이 살아있는지 확인합니다.
	// 실패 시 leader 자격 상실로 간주.
	Heartbeat(ctx context.Context) error

	// Release는 advisory lock을 해제하고 conn을 반환합니다.
	// 멱등 (이미 해제된 상태에서 호출해도 nil).
	Release(ctx context.Context) error
}

// Manager는 leader-election + role 상태를 관리합니다.
//
// 사용:
//
//	mgr := ha.NewManager(pgLock, "host-a:1234", 5*time.Second, logger)
//	mgr.OnLeaderAcquired(func() { scheduler.Start(...) })
//	mgr.OnLeaderLost(func() { scheduler.Stop() })
//	mgr.Start(ctx)
//	defer mgr.Stop(context.Background())
//
// 동시성: Start는 한 번만 호출. State()/IsLeader()/Subscribe는 thread-safe.
type Manager struct {
	lock     Lock
	leaderID string
	interval time.Duration
	logger   Logger

	role          atomic.Int32
	epoch         atomic.Int64
	lastHeartbeat atomic.Pointer[time.Time]

	mu         sync.Mutex
	onAcquired []func()
	onLost     []func()

	startOnce sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// Logger는 platform/logger 의존을 피하기 위한 minimal slog 호환 인터페이스입니다.
// bootstrap에서 *slog.Logger 어댑터를 주입합니다.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// NewManager는 Manager를 생성합니다.
//
// interval이 0이면 기본 5초.
// logger가 nil이면 noopLogger 사용.
func NewManager(lock Lock, leaderID string, interval time.Duration, logger Logger) *Manager {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if logger == nil {
		logger = noopLogger{}
	}
	m := &Manager{
		lock:     lock,
		leaderID: leaderID,
		interval: interval,
		logger:   logger,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	now := time.Time{}
	m.lastHeartbeat.Store(&now)
	return m
}

// Role은 현재 역할 스냅샷입니다.
func (m *Manager) Role() Role {
	return Role(m.role.Load())
}

// CurrentEpoch는 보유 중인 fence token입니다 (follower면 0).
func (m *Manager) CurrentEpoch() int64 {
	return m.epoch.Load()
}

// IsLeader는 RoleProvider 구현입니다.
func (m *Manager) IsLeader() bool {
	return m.Role() == RoleLeader
}

// LeaderID는 본 인스턴스의 ID입니다 (정적 값).
func (m *Manager) LeaderID() string {
	return m.leaderID
}

// LastHeartbeatAt은 마지막 heartbeat tick의 시각입니다.
func (m *Manager) LastHeartbeatAt() time.Time {
	if t := m.lastHeartbeat.Load(); t != nil {
		return *t
	}
	return time.Time{}
}

// State는 현재 상태의 스냅샷을 반환합니다 (/healthz·메트릭용).
func (m *Manager) State() State {
	return State{
		Enabled:         true,
		Role:            m.Role(),
		Epoch:           m.CurrentEpoch(),
		LeaderID:        m.leaderID,
		LastHeartbeatAt: m.LastHeartbeatAt(),
	}
}

// OnLeaderAcquired는 leader 승격 시 호출될 callback을 등록합니다.
// callback은 heartbeat goroutine에서 동기 실행 — 빠르게 반환하거나 자체 goroutine 분리 권장.
func (m *Manager) OnLeaderAcquired(fn func()) {
	if fn == nil {
		return
	}
	m.mu.Lock()
	m.onAcquired = append(m.onAcquired, fn)
	m.mu.Unlock()
}

// OnLeaderLost는 leader 자격 상실 시 호출될 callback을 등록합니다.
func (m *Manager) OnLeaderLost(fn func()) {
	if fn == nil {
		return
	}
	m.mu.Lock()
	m.onLost = append(m.onLost, fn)
	m.mu.Unlock()
}

// Start는 heartbeat goroutine을 시작합니다.
// 한 번만 호출. 추가 호출은 무시됩니다.
func (m *Manager) Start(ctx context.Context) {
	m.startOnce.Do(func() {
		go m.run(ctx)
	})
}

// Stop은 heartbeat 정지 + lock release를 수행합니다.
// 멱등 — 여러 번 호출해도 안전.
func (m *Manager) Stop(ctx context.Context) error {
	select {
	case <-m.stopCh:
		// 이미 정지 중
	default:
		close(m.stopCh)
	}
	// goroutine 종료 대기
	select {
	case <-m.doneCh:
	case <-ctx.Done():
		return ctx.Err()
	}
	// 자원 해제
	releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return m.lock.Release(releaseCtx)
}

func (m *Manager) run(ctx context.Context) {
	defer close(m.doneCh)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// 첫 시도는 즉시 (interval 대기 없이) — 부팅 직후 leader 결정 빠르게.
	m.tick(ctx)

	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

func (m *Manager) tick(ctx context.Context) {
	now := time.Now()
	m.lastHeartbeat.Store(&now)

	if m.IsLeader() {
		// leader: heartbeat ping
		if err := m.lock.Heartbeat(ctx); err != nil {
			m.logger.Warn("ha: heartbeat failed, demoting to follower", "err", err, "epoch", m.CurrentEpoch())
			m.demote()
		}
		return
	}

	// follower: lock 시도
	acquired, epoch, err := m.lock.TryAcquire(ctx, m.leaderID)
	if err != nil {
		m.logger.Warn("ha: TryAcquire failed", "err", err)
		return
	}
	if acquired {
		m.promote(epoch)
	}
}

func (m *Manager) promote(epoch int64) {
	m.epoch.Store(epoch)
	m.role.Store(int32(RoleLeader))
	m.logger.Info("ha: promoted to leader", "epoch", epoch, "leaderId", m.leaderID)

	m.mu.Lock()
	callbacks := append([]func(){}, m.onAcquired...)
	m.mu.Unlock()
	for _, fn := range callbacks {
		safeCall(fn, m.logger, "onLeaderAcquired")
	}
}

func (m *Manager) demote() {
	prevEpoch := m.epoch.Load()
	m.role.Store(int32(RoleFollower))
	m.epoch.Store(0)
	m.logger.Info("ha: demoted to follower", "prevEpoch", prevEpoch)

	// lock 자원 정리 (이미 conn 끊겼을 수 있지만 멱등)
	releaseCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = m.lock.Release(releaseCtx)

	m.mu.Lock()
	callbacks := append([]func(){}, m.onLost...)
	m.mu.Unlock()
	for _, fn := range callbacks {
		safeCall(fn, m.logger, "onLeaderLost")
	}
}

func safeCall(fn func(), logger Logger, name string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("ha: callback panic", "callback", name, "panic", r)
		}
	}()
	fn()
}

type noopLogger struct{}

func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}

// 컴파일 시점 인터페이스 매칭 보증.
var _ RoleProvider = (*Manager)(nil)
