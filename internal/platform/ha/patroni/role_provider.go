// Package patroni는 Patroni REST endpoint를 polling해 leader 여부 + timeline(epoch)을
// 노출하는 RoleProvider를 구현합니다 (Phase 9 Stage 9.3, D-AF-1~4).
//
// 동작:
//  1. Start(ctx)가 goroutine으로 ticker 진입 (default 1초)
//  2. 매 tick에 GET <PatroniURL>/cluster JSON 파싱
//  3. members[].role == "master" 또는 leader field에서 leader pod name 추출
//  4. leader == local hostname이면 IsLeader=true, timeline → epoch
//  5. atomic.Bool / atomic.Int64로 race-safe 노출
//
// 동시 만족 interface (duck typing):
//   - audit.RoleProvider (IsLeader + CurrentEpoch)
//   - lagmetric.RoleProvider (IsLeader)
//   - cronsched.RoleProvider (IsLeader)
//
// 본 패키지는 ha.Manager(E25 PG advisory lock)와 별 layer — bootstrap에서 `--ha-rp=patroni`
// 시 본 RoleProvider를 audit + lagmetric + cronsched에 주입하면 E25 대신 Patroni가
// leader-election 단일 source of truth.
//
// air-gap customer는 `--ha-rp=e25`로 기존 ha.Manager 유지 (D-AF-4 fallback).
package patroni

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultPollInterval은 Patroni /cluster polling 기본 간격입니다.
//
// 1초는 Patroni의 leader lease TTL(30초 기본)에 비해 빠른 detection 보장.
// customer가 cluster API 부담을 줄이려면 PollInterval=5s 등으로 조정 가능.
const DefaultPollInterval = 1 * time.Second

// DefaultRequestTimeout은 단일 Patroni REST 호출 timeout입니다.
const DefaultRequestTimeout = 3 * time.Second

// Deps는 RoleProvider 의존성입니다.
type Deps struct {
	// PatroniURL은 Patroni REST endpoint base URL입니다 (예: http://patroni:8008).
	// trailing slash 무관 — 본 패키지가 정규화.
	PatroniURL string
	// LocalHostname은 본 노드의 식별자 — Patroni /cluster의 leader name과 비교.
	// Kubernetes 환경에서는 보통 Pod name (예: rosshield-server-0).
	// 빈 값이면 os.Hostname() fallback (Bootstrap이 명시 권장).
	LocalHostname string
	// PollInterval은 ticker 간격. 0이면 DefaultPollInterval (1s).
	PollInterval time.Duration
	// RequestTimeout은 단일 HTTP 호출 timeout. 0이면 DefaultRequestTimeout (3s).
	RequestTimeout time.Duration
	// HTTPClient는 customer 환경별 transport 주입 — nil이면 http.DefaultClient 사용.
	HTTPClient *http.Client
	// Logger는 polling 실패 logging. nil이면 slog.Default().
	Logger *slog.Logger
}

// RoleProvider는 Patroni REST polling 기반 HA RoleProvider 구현입니다.
//
// audit.RoleProvider · lagmetric.RoleProvider · cronsched.RoleProvider duck-typed로 자동 만족.
type RoleProvider struct {
	deps   Deps
	url    string // 정규화된 base URL
	leader atomic.Bool
	epoch  atomic.Int64
	closed chan struct{}
}

// New는 RoleProvider를 만듭니다.
//
// PatroniURL · LocalHostname 필수. 빈 값이면 error.
func New(deps Deps) (*RoleProvider, error) {
	if strings.TrimSpace(deps.PatroniURL) == "" {
		return nil, errors.New("patroni: PatroniURL required")
	}
	if strings.TrimSpace(deps.LocalHostname) == "" {
		return nil, errors.New("patroni: LocalHostname required (보통 Pod name)")
	}
	if deps.PollInterval <= 0 {
		deps.PollInterval = DefaultPollInterval
	}
	if deps.RequestTimeout <= 0 {
		deps.RequestTimeout = DefaultRequestTimeout
	}
	if deps.HTTPClient == nil {
		deps.HTTPClient = &http.Client{Timeout: deps.RequestTimeout}
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	url := strings.TrimRight(deps.PatroniURL, "/")
	return &RoleProvider{
		deps:   deps,
		url:    url,
		closed: make(chan struct{}),
	}, nil
}

// IsLeader는 본 인스턴스가 Patroni leader인지 반환합니다 (race-safe).
//
// Start 호출 전 또는 첫 polling 전에는 false 반환 — 부팅 직후 안전한 default.
func (rp *RoleProvider) IsLeader() bool {
	return rp.leader.Load()
}

// CurrentEpoch는 현재 Patroni timeline을 반환합니다 (audit fence token).
//
// Patroni timeline은 PG promote 시 자동 증가 + replication에 자동 포함 — Lodestar의
// leader_epoch column에 그대로 저장하면 cross-region propagation 무료.
func (rp *RoleProvider) CurrentEpoch() int64 {
	return rp.epoch.Load()
}

// Start는 ticker goroutine을 백그라운드로 실행합니다.
//
// 첫 polling은 즉시 수행 (부팅 직후 leader 식별 보장).
func (rp *RoleProvider) Start(ctx context.Context) {
	go rp.loop(ctx)
}

// Close는 collector goroutine 종료를 기다립니다 (graceful shutdown).
func (rp *RoleProvider) Close() {
	<-rp.closed
}

func (rp *RoleProvider) loop(ctx context.Context) {
	defer close(rp.closed)

	rp.pollOnce(ctx)

	ticker := time.NewTicker(rp.deps.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rp.pollOnce(ctx)
		}
	}
}

// clusterResponse는 Patroni /cluster endpoint 응답 partial schema입니다.
//
// Patroni 3.x 표준 응답:
//
//	{
//	  "members": [{"name": "pod-0", "role": "master", "state": "running"}, ...],
//	  "leader": "pod-0",
//	  "timeline": 42
//	}
//
// 일부 Patroni 버전은 leader field 없이 members[].role=="master"로만 식별 — 본 코드는
// 양쪽 모두 cover.
type clusterResponse struct {
	Members  []memberInfo `json:"members"`
	Leader   string       `json:"leader"`
	Timeline int64        `json:"timeline"`
}

type memberInfo struct {
	Name  string `json:"name"`
	Role  string `json:"role"`
	State string `json:"state"`
}

// pollOnce는 한 번의 Patroni /cluster polling을 수행합니다 (test에서 직접 호출 가능).
//
// 동작:
//  1. GET <url>/cluster (RequestTimeout)
//  2. JSON 파싱 → clusterResponse
//  3. leader name 결정 (Leader field 우선, 부재 시 members[].role=="master" fallback)
//  4. leader == LocalHostname이면 IsLeader=true
//  5. Timeline → epoch (atomic.Store)
//
// 호출 실패는 logger.Warn — atomic 값은 변경 안 함 (직전 상태 유지).
func (rp *RoleProvider) pollOnce(ctx context.Context) {
	reqCtx, cancel := context.WithTimeout(ctx, rp.deps.RequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rp.url+"/cluster", nil)
	if err != nil {
		rp.deps.Logger.Warn("patroni: NewRequest failed", "err", err.Error())
		return
	}
	req.Header.Set("Accept", "application/json")

	resp, err := rp.deps.HTTPClient.Do(req)
	if err != nil {
		rp.deps.Logger.Warn("patroni: HTTP Do failed", "err", err.Error(), "url", rp.url)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		rp.deps.Logger.Warn("patroni: non-200 status",
			"status", resp.StatusCode, "body", string(body))
		return
	}

	var cluster clusterResponse
	if err := json.NewDecoder(resp.Body).Decode(&cluster); err != nil {
		rp.deps.Logger.Warn("patroni: JSON decode failed", "err", err.Error())
		return
	}

	leaderName := resolveLeader(cluster)
	isLeader := leaderName != "" && leaderName == rp.deps.LocalHostname

	rp.leader.Store(isLeader)
	rp.epoch.Store(cluster.Timeline)
}

// resolveLeader는 cluster 응답에서 leader pod name을 결정합니다.
//
// 우선순위:
//  1. cluster.Leader field (Patroni 3.x 표준)
//  2. members[].role == "master" fallback (일부 버전 호환)
//
// 둘 다 부재 시 빈 문자열 — IsLeader=false로 fallback.
func resolveLeader(c clusterResponse) string {
	if c.Leader != "" {
		return c.Leader
	}
	for _, m := range c.Members {
		if strings.EqualFold(m.Role, "master") || strings.EqualFold(m.Role, "primary") {
			return m.Name
		}
	}
	return ""
}
