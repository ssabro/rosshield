package sshpool

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// PoolConfig는 Pool 동시성·재시도 옵션입니다 (R4-1).
//
// Phase 1 단순화: per-host + per-tenant limit만 (per-key는 host의 sub-case로 흡수).
// idle 재사용은 Phase 2+ — Stage B는 매 Acquire마다 dial → release 시 close
// (health check 부담 0, conn 누수 0). 이 정책은 부하 테스트(E6 exit)에서 재검토.
type PoolConfig struct {
	PerHostLimit   int           // 한 host:port 동시 conn 수, 0 → DefaultPerHostLimit (5)
	PerTenantLimit int           // 한 tenant 동시 conn 수, 0 → DefaultPerTenantLimit (50)
	DialTimeout    time.Duration // 0 → DefaultDialTimeout
	DialMaxRetries int           // 재시도 횟수, 0 → DefaultDialMaxRetries (3)
	DialBaseDelay  time.Duration // backoff 초기 대기, 0 → DefaultDialBaseDelay (200ms)
}

// Pool 기본값.
const (
	DefaultPerHostLimit   = 5
	DefaultPerTenantLimit = 50
	DefaultDialMaxRetries = 3
	DefaultDialBaseDelay  = 200 * time.Millisecond
)

// PoolKey는 limit 카운팅을 위한 식별자입니다.
//
// TenantID는 per-tenant limit, Host:Port는 per-host limit, KeyID는 동일 자격증명 추적용
// (Phase 1은 limit에 직접 사용 안 하지만 메타데이터로 보존 — Phase 2+에서 per-key limit 도입 시 활용).
type PoolKey struct {
	TenantID string
	KeyID    string // 자격증명 식별자 (e.g. credential.kek_id)
	Host     string
	Port     int
}

func (k PoolKey) hostKey() string {
	return k.Host + ":" + strconv.Itoa(k.Port)
}

// Pool은 SSH connection 풀 표면입니다 (R4-1).
//
// Phase 1 단순화: dial-on-acquire, close-on-release. idle 재사용은 Phase 2+.
// Pool의 역할은 동시성 제한(per-host·per-tenant) + dial backoff.
type Pool interface {
	// Acquire는 target에 SSH 연결을 수립하고 client + release함수를 반환합니다.
	// per-host 또는 per-tenant limit 도달 시 ctx 만료까지 대기.
	// release()는 idempotent — 두 번째 호출 시 no-op.
	Acquire(ctx context.Context, key PoolKey, target Target) (*ssh.Client, ReleaseFunc, error)

	// Close는 새 Acquire를 거부하고 현재 사용 중인 conn은 release 시 close됩니다.
	Close() error
}

// ReleaseFunc은 Pool에서 받은 conn을 반환합니다 (close).
type ReleaseFunc func()

// 공통 에러.
var (
	ErrPoolClosed = errors.New("sshpool: pool is closed")
)

type pool struct {
	cfg PoolConfig

	mu       sync.Mutex
	closed   bool
	hostSems map[string]chan struct{} // hostKey → semaphore channel (capacity = PerHostLimit)
	tenSems  map[string]chan struct{} // tenantID → semaphore (capacity = PerTenantLimit)
	dialFunc dialFunc                 // 테스트에서 swap 가능 (TCP dial 로직)
}

// dialFunc은 Pool이 SSH 연결을 수립할 때 사용하는 함수입니다.
// 테스트(fakesshd)에서 swap 가능 — 실제 구현은 net.Dial+ssh.NewClientConn.
type dialFunc func(ctx context.Context, target Target, dialTimeout time.Duration) (*ssh.Client, error)

// NewPool은 새 Pool을 반환합니다 (기본값 자동 적용).
func NewPool(cfg PoolConfig) Pool {
	if cfg.PerHostLimit == 0 {
		cfg.PerHostLimit = DefaultPerHostLimit
	}
	if cfg.PerTenantLimit == 0 {
		cfg.PerTenantLimit = DefaultPerTenantLimit
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = DefaultDialTimeout
	}
	if cfg.DialMaxRetries == 0 {
		cfg.DialMaxRetries = DefaultDialMaxRetries
	}
	if cfg.DialBaseDelay == 0 {
		cfg.DialBaseDelay = DefaultDialBaseDelay
	}
	return &pool{
		cfg:      cfg,
		hostSems: make(map[string]chan struct{}),
		tenSems:  make(map[string]chan struct{}),
		dialFunc: realDial,
	}
}

// Acquire는 Pool.Acquire 구현입니다.
//
// 동시성 모델: 채널 semaphore로 per-host·per-tenant limit 강제.
// ctx cancel 시 슬롯 즉시 반환(누수 방지).
func (p *pool) Acquire(ctx context.Context, key PoolKey, target Target) (*ssh.Client, ReleaseFunc, error) {
	if err := validateTarget(target); err != nil {
		return nil, nil, err
	}

	// closed 체크.
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, nil, ErrPoolClosed
	}
	hostSem := p.semFor(p.hostSems, key.hostKey(), p.cfg.PerHostLimit)
	var tenSem chan struct{}
	if key.TenantID != "" {
		tenSem = p.semFor(p.tenSems, key.TenantID, p.cfg.PerTenantLimit)
	}
	p.mu.Unlock()

	// per-tenant 슬롯 (있으면 먼저 — broader limit이 먼저 차단되는 패턴).
	if tenSem != nil {
		select {
		case tenSem <- struct{}{}:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}

	// per-host 슬롯.
	select {
	case hostSem <- struct{}{}:
	case <-ctx.Done():
		if tenSem != nil {
			<-tenSem
		}
		return nil, nil, ctx.Err()
	}

	// dial with backoff.
	client, err := p.dialWithBackoff(ctx, target)
	if err != nil {
		<-hostSem
		if tenSem != nil {
			<-tenSem
		}
		return nil, nil, err
	}

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			_ = client.Close()
			<-hostSem
			if tenSem != nil {
				<-tenSem
			}
		})
	}
	return client, release, nil
}

// Close는 Pool.Close 구현입니다 (idempotent).
func (p *pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	// 사용 중 conn은 release 시 close됨. Pool 자체는 더 이상 새 Acquire 받지 않음.
	return nil
}

// semFor는 식별자별 semaphore 채널을 lazy-create합니다 (호출자는 mu lock 보유).
func (p *pool) semFor(m map[string]chan struct{}, id string, limit int) chan struct{} {
	if sem, ok := m[id]; ok {
		return sem
	}
	sem := make(chan struct{}, limit)
	m[id] = sem
	return sem
}

// dialWithBackoff는 jittered exponential backoff로 dial을 재시도합니다 (R4-1).
//
// 시도 횟수 = 1 + DialMaxRetries (1 회 초기 + 최대 DialMaxRetries 회 재시도).
// 대기: base * 2^attempt + jitter [0, base/2).
func (p *pool) dialWithBackoff(ctx context.Context, target Target) (*ssh.Client, error) {
	var lastErr error
	totalAttempts := 1 + p.cfg.DialMaxRetries
	for attempt := 0; attempt < totalAttempts; attempt++ {
		if attempt > 0 {
			delay := p.cfg.DialBaseDelay * (1 << (attempt - 1))
			jitter := time.Duration(rand.Int64N(int64(p.cfg.DialBaseDelay / 2)))
			select {
			case <-time.After(delay + jitter):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		client, err := p.dialFunc(ctx, target, p.cfg.DialTimeout)
		if err == nil {
			return client, nil
		}
		lastErr = err
		// ctx cancel은 즉시 종료.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("sshpool: dial failed after %d attempts: %w", totalAttempts, lastErr)
}

// realDial은 실 환경의 dial 구현입니다 (테스트에서 dialFunc swap 가능).
func realDial(ctx context.Context, target Target, dialTimeout time.Duration) (*ssh.Client, error) {
	addr := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	dialer := &net.Dialer{Timeout: dialTimeout}
	netConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	config := &ssh.ClientConfig{
		User:            target.Username,
		Auth:            []ssh.AuthMethod{target.Auth},
		HostKeyCallback: target.HostKeyCallback,
		Timeout:         dialTimeout,
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, addr, config)
	if err != nil {
		_ = netConn.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}
