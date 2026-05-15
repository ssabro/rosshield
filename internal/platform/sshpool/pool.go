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
// scanrun SSH 통합 Stage 4 — idle 재사용 + IdleTimeout eviction + keepalive 추가.
// IdleTimeout=0이면 idle 재사용 비활성(Phase 1 호환 모드: dial-on-acquire, close-on-release).
// IdleTimeout>0 시 release된 conn은 idle 풀로 반환되어 다음 같은 PoolKey Acquire가 재사용.
type PoolConfig struct {
	PerHostLimit   int           // 한 host:port 동시 conn 수, 0 → DefaultPerHostLimit (5)
	PerTenantLimit int           // 한 tenant 동시 conn 수, 0 → DefaultPerTenantLimit (50)
	DialTimeout    time.Duration // 0 → DefaultDialTimeout
	DialMaxRetries int           // 재시도 횟수, 0 → DefaultDialMaxRetries (3)
	DialBaseDelay  time.Duration // backoff 초기 대기, 0 → DefaultDialBaseDelay (200ms)

	// Stage 4 — idle 재사용.
	IdleTimeout       time.Duration // 0 → idle 재사용 비활성 (기존 dial-on-acquire 호환).
	KeepaliveInterval time.Duration // 0 → DefaultKeepaliveInterval (30s). idle conn 헬스체크 주기.

	// Metrics는 nil 허용 — emit 없이 동작 (단위 테스트 격리).
	Metrics PoolMetrics
}

// Pool 기본값.
const (
	DefaultPerHostLimit      = 5
	DefaultPerTenantLimit    = 50
	DefaultDialMaxRetries    = 3
	DefaultDialBaseDelay     = 200 * time.Millisecond
	DefaultKeepaliveInterval = 30 * time.Second
)

// PoolMetrics는 Pool이 emit하는 metric 표면입니다 (P5 — metrics 패키지 직접 import 회피).
//
// bootstrap이 metrics.Registry → PoolMetrics 어댑터로 주입. nil 허용(테스트).
type PoolMetrics interface {
	// IncDial은 dial 시도 결과를 카운트합니다. result = "ok" | "fail".
	IncDial(result string)
	// SetIdleConns는 현재 idle 풀 크기 gauge를 set합니다.
	SetIdleConns(n int)
}

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

	// Stage 4 — idle pool. key는 PoolKey 직렬화, value는 LIFO stack of pooledConn.
	// IdleTimeout=0이면 미사용. mu 잠금 보유 시 접근.
	idle map[string][]*pooledConn

	// keepalive goroutine 종료 신호. nil이면 keepalive 비활성.
	stopKeepalive chan struct{}
}

// pooledConn은 idle 풀에 저장되는 client + 마지막 사용 시각입니다.
type pooledConn struct {
	client     *ssh.Client
	lastUsedAt time.Time
}

// dialFunc은 Pool이 SSH 연결을 수립할 때 사용하는 함수입니다.
// 테스트(fakesshd)에서 swap 가능 — 실제 구현은 net.Dial+ssh.NewClientConn.
type dialFunc func(ctx context.Context, target Target, dialTimeout time.Duration) (*ssh.Client, error)

// NewPool은 새 Pool을 반환합니다 (기본값 자동 적용).
//
// IdleTimeout > 0이면 idle 재사용 + keepalive goroutine 시작.
// Close 호출 시 keepalive goroutine 종료 + idle conn 모두 close.
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
	if cfg.IdleTimeout > 0 && cfg.KeepaliveInterval == 0 {
		cfg.KeepaliveInterval = DefaultKeepaliveInterval
	}
	p := &pool{
		cfg:      cfg,
		hostSems: make(map[string]chan struct{}),
		tenSems:  make(map[string]chan struct{}),
		dialFunc: realDial,
		idle:     make(map[string][]*pooledConn),
	}
	if cfg.IdleTimeout > 0 {
		p.stopKeepalive = make(chan struct{})
		go p.keepaliveLoop()
	}
	return p
}

// idleKey는 idle 풀에서 conn을 식별하는 key를 만듭니다.
//
// (TenantID, KeyID, Host, Port) 모두 일치해야 같은 conn으로 간주 — 자격증명 또는
// host key 변경 시 stale conn 재사용 회피.
func idleKey(k PoolKey) string {
	return k.TenantID + "|" + k.KeyID + "|" + k.hostKey()
}

// Acquire는 Pool.Acquire 구현입니다.
//
// 동시성 모델: 채널 semaphore로 per-host·per-tenant limit 강제.
// ctx cancel 시 슬롯 즉시 반환(누수 방지).
//
// scanrun SSH 통합 Stage 4 — IdleTimeout > 0이면 idle 풀에서 재사용 시도, miss 시 dial.
// release 시 conn이 살아있으면 idle 풀로 반환, 죽었거나 IdleTimeout=0이면 close.
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

	// Stage 4 — idle 풀에서 재사용 시도(IdleTimeout > 0인 경우만).
	var client *ssh.Client
	if p.cfg.IdleTimeout > 0 {
		client = p.popIdle(key)
	}

	if client == nil {
		// miss — dial.
		var err error
		client, err = p.dialWithBackoff(ctx, target)
		if p.cfg.Metrics != nil {
			if err != nil {
				p.cfg.Metrics.IncDial("fail")
			} else {
				p.cfg.Metrics.IncDial("ok")
			}
		}
		if err != nil {
			<-hostSem
			if tenSem != nil {
				<-tenSem
			}
			return nil, nil, err
		}
	}

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			// Stage 4 — IdleTimeout > 0면 살아있는 conn은 idle 풀로 반환.
			if p.cfg.IdleTimeout > 0 && isAlive(client) {
				p.pushIdle(key, client)
			} else {
				_ = client.Close()
			}
			<-hostSem
			if tenSem != nil {
				<-tenSem
			}
		})
	}
	return client, release, nil
}

// popIdle은 key에 해당하는 idle conn을 LIFO로 꺼냅니다.
//
// 만료된(IdleTimeout 초과) conn은 자동 close + skip — 다음 conn 시도. 모두 만료면 nil 반환.
func (p *pool) popIdle(key PoolKey) *ssh.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := idleKey(key)
	stack := p.idle[k]
	now := time.Now()
	for len(stack) > 0 {
		// LIFO — 최근 사용된 conn 우선(cache locality 비유).
		conn := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if now.Sub(conn.lastUsedAt) > p.cfg.IdleTimeout {
			_ = conn.client.Close()
			continue
		}
		p.idle[k] = stack
		p.updateIdleGaugeLocked()
		return conn.client
	}
	delete(p.idle, k)
	p.updateIdleGaugeLocked()
	return nil
}

// pushIdle은 idle 풀에 conn을 반환합니다 (LIFO).
//
// closed 상태면 즉시 conn close + skip.
func (p *pool) pushIdle(key PoolKey, client *ssh.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = client.Close()
		return
	}
	k := idleKey(key)
	p.idle[k] = append(p.idle[k], &pooledConn{
		client:     client,
		lastUsedAt: time.Now(),
	})
	p.updateIdleGaugeLocked()
}

// updateIdleGaugeLocked는 모든 PoolKey idle 합계를 metrics gauge에 반영합니다.
//
// 호출자는 mu lock 보유. nil metrics는 no-op.
func (p *pool) updateIdleGaugeLocked() {
	if p.cfg.Metrics == nil {
		return
	}
	total := 0
	for _, stack := range p.idle {
		total += len(stack)
	}
	p.cfg.Metrics.SetIdleConns(total)
}

// isAlive는 client가 사용 가능한지 keepalive request로 확인합니다.
//
// SendRequest("keepalive@openssh.com", true, nil)는 OpenSSH 표준 — wantReply=true로 응답 필수.
// 실패 시 conn 사용 불가 → close 권장.
func isAlive(client *ssh.Client) bool {
	if client == nil {
		return false
	}
	_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}

// keepaliveLoop는 주기적으로 idle conn 헬스체크 + 만료 eviction을 수행합니다.
//
// IdleTimeout > 0인 경우만 NewPool에서 시작. Close 시 stopKeepalive 통해 종료.
func (p *pool) keepaliveLoop() {
	ticker := time.NewTicker(p.cfg.KeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopKeepalive:
			return
		case <-ticker.C:
			p.evictExpiredAndDead()
		}
	}
}

// evictExpiredAndDead는 idle 풀을 한 번 스캔해 만료·죽은 conn을 close합니다.
func (p *pool) evictExpiredAndDead() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for k, stack := range p.idle {
		filtered := stack[:0]
		for _, conn := range stack {
			if now.Sub(conn.lastUsedAt) > p.cfg.IdleTimeout {
				_ = conn.client.Close()
				continue
			}
			if !isAlive(conn.client) {
				_ = conn.client.Close()
				continue
			}
			filtered = append(filtered, conn)
		}
		if len(filtered) == 0 {
			delete(p.idle, k)
		} else {
			p.idle[k] = filtered
		}
	}
	p.updateIdleGaugeLocked()
}

// Close는 Pool.Close 구현입니다 (idempotent).
//
// keepalive goroutine 종료 + idle conn 모두 close. 사용 중 conn은 release 시 close.
func (p *pool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	stopCh := p.stopKeepalive
	idle := p.idle
	p.idle = nil
	p.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
	for _, stack := range idle {
		for _, conn := range stack {
			_ = conn.client.Close()
		}
	}
	if p.cfg.Metrics != nil {
		p.cfg.Metrics.SetIdleConns(0)
	}
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
