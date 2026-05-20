// Package lagmetric은 PG primary의 pg_stat_replication view를 polling하여
// `rosshield_replication_lag_seconds` Prometheus metric을 emit합니다 (Phase 8 MR.T8).
//
// 동작:
//   - 30초 간격 ticker (또는 cfg.Interval) → SELECT application_name, replay_lag FROM
//     pg_stat_replication
//   - subscriber별 lag(초)을 metric.ReplicationLagSeconds Gauge에 Set
//   - subscriber가 사라지면 그 label의 metric도 자동 삭제 (DeletePartialMatch)
//
// 조건:
//   - PG storage + replication enabled + primary role 조합에서만 실행 (bootstrap이 분기)
//   - HA leader-only는 본 collector에서 직접 처리 안 함 — primary 전체 collector 1개 가정
//     (HA 활성 환경에서는 leader 단일 인스턴스만 emit 권장, cronsched RoleProvider 패턴
//     별 적용은 후속 carryover)
//
// 도메인 격리: lagmetric은 platform replication 하위 — cfg + pgxpool + metrics만 의존.
package lagmetric

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ssabro/rosshield/internal/platform/metrics"
)

// Querier는 Collector가 호출하는 pgxpool.Pool의 최소 표면입니다.
//
// 작은 interface로 fake mock 단위 test 가능. *pgxpool.Pool은 자동 만족.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// RoleProvider는 HA leader 여부를 질의하는 최소 interface입니다.
//
// cronsched.RoleProvider와 동등 signature — ha.Manager가 자동 만족. nil이면 single-
// instance 가정 (모든 polling 수행). HA cluster에서 follower instance가 metric 중복
// emit하지 않도록 본 gate가 필수.
type RoleProvider interface {
	IsLeader() bool
}

// DefaultInterval은 polling 간격 기본값입니다.
const DefaultInterval = 30 * time.Second

// Deps는 Collector 의존성입니다.
type Deps struct {
	Querier  Querier           // primary PG querier — pg_stat_replication 조회 (보통 pgxpool.Pool)
	Registry *metrics.Registry // ReplicationLagSeconds Gauge emit 대상
	Interval time.Duration     // 0이면 DefaultInterval (30s)
	Logger   *slog.Logger
	// Role은 HA cluster에서 leader instance만 metric emit하도록 follower tick을 silent
	// skip합니다. nil이면 single-instance 가정 (모든 polling 수행).
	Role RoleProvider
}

// Collector는 pg_stat_replication을 polling해 lag metric을 emit합니다.
//
// HA gate는 deps.Role 또는 SetRoleProvider lazy 주입으로 follower tick을 silent skip.
// goroutine과 SetRoleProvider 동시 호출 안전성을 위해 atomic.Value로 보호.
type Collector struct {
	deps   Deps
	role   atomic.Value // RoleProvider (lazy, nil 가능)
	closed chan struct{}
}

// New는 Collector를 만듭니다.
//
// Querier · Registry 필수. Interval이 0이면 30초 기본 적용. Logger 0이면 slog.Default.
func New(deps Deps) (*Collector, error) {
	if deps.Querier == nil {
		return nil, errors.New("lagmetric: Querier required")
	}
	if deps.Registry == nil {
		return nil, errors.New("lagmetric: Registry required")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Interval <= 0 {
		deps.Interval = DefaultInterval
	}
	c := &Collector{
		deps:   deps,
		closed: make(chan struct{}),
	}
	if deps.Role != nil {
		c.role.Store(deps.Role)
	}
	return c, nil
}

// SetRoleProvider는 HA Manager를 lazy 주입합니다 (cronsched.SetRoleProvider 패턴).
//
// Bootstrap 흐름: lagCollector New → Start 후 HA Manager 결선되면 SetRoleProvider 호출.
// nil 호출은 Role 비활성화 (single-instance 가정 복귀).
func (c *Collector) SetRoleProvider(rp RoleProvider) {
	if rp == nil {
		c.role.Store(RoleProvider(nilRoleSentinel{}))
		return
	}
	c.role.Store(rp)
}

// nilRoleSentinel은 atomic.Value에 nil interface를 직접 Store할 수 없는 제약을 회피하는
// sentinel입니다. IsLeader=true로 항상 leader 반환 (single-instance 가정 일관).
type nilRoleSentinel struct{}

func (nilRoleSentinel) IsLeader() bool { return true }

// currentRole은 atomic.Value에서 현재 RoleProvider를 읽습니다 (race-safe).
func (c *Collector) currentRole() RoleProvider {
	v := c.role.Load()
	if v == nil {
		return nil
	}
	if _, ok := v.(nilRoleSentinel); ok {
		return nil
	}
	rp, _ := v.(RoleProvider)
	return rp
}

// Start는 ticker goroutine을 백그라운드로 실행합니다. ctx 종료 시 자동 정지.
//
// 첫 polling은 ticker 첫 tick이 아닌 즉시 1회 실행 — 부팅 직후 metric 노출 보장.
func (c *Collector) Start(ctx context.Context) {
	go c.loop(ctx)
}

// Close는 collector goroutine 종료를 기다립니다 (graceful shutdown).
func (c *Collector) Close() {
	<-c.closed
}

func (c *Collector) loop(ctx context.Context) {
	defer close(c.closed)

	// 첫 polling 즉시 — 부팅 직후 metric 노출.
	c.pollOnce(ctx)

	ticker := time.NewTicker(c.deps.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pollOnce(ctx)
		}
	}
}

// pollOnce는 한 번의 polling을 수행합니다 (test에서도 직접 호출 가능).
//
// pg_stat_replication.replay_lag은 interval 타입 — EXTRACT(EPOCH FROM ...)로 초 변환.
// replay_lag이 NULL(예: 신규 connection, traffic 부재)인 경우 0으로 처리.
//
// HA gate: Role != nil + IsLeader()=false면 polling skip + Gauge.Reset() — follower
// instance가 metric 중복 emit 방지. leader 단일 인스턴스만 cardinality 가짐.
//
// 조회 실패는 logger.Warn만 — 부분 실패가 collector를 중단시키지 않음.
//
// stale label 처리: 매 polling 시작 시 Gauge.Reset() — 사라진 subscriber label 자동
// 제거 + 등장한 subscriber만 다시 Set. 30초 간격 polling이라 cardinality 영향 미미.
func (c *Collector) pollOnce(ctx context.Context) {
	// HA gate: follower면 metric 비움 + skip. atomic.Value로 SetRoleProvider lazy 주입과
	// pollOnce goroutine 동시 호출 안전.
	if role := c.currentRole(); role != nil && !role.IsLeader() {
		c.deps.Registry.ReplicationLagSeconds.Reset()
		return
	}

	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	const querySQL = `
SELECT application_name,
       COALESCE(EXTRACT(EPOCH FROM replay_lag), 0)::float8 AS lag_sec
FROM pg_stat_replication
`
	rows, err := c.deps.Querier.Query(queryCtx, querySQL)
	if err != nil {
		c.deps.Logger.Warn("lagmetric: query pg_stat_replication failed",
			"err", err.Error())
		return
	}
	defer rows.Close()

	// 시작 시 reset — 사라진 subscriber label 자동 cleanup.
	c.deps.Registry.ReplicationLagSeconds.Reset()

	for rows.Next() {
		var (
			appName string
			lagSec  float64
		)
		if err := rows.Scan(&appName, &lagSec); err != nil {
			c.deps.Logger.Warn("lagmetric: row scan failed", "err", err.Error())
			continue
		}
		c.deps.Registry.ReplicationLagSeconds.WithLabelValues(appName).Set(lagSec)
	}
	if err := rows.Err(); err != nil {
		c.deps.Logger.Warn("lagmetric: rows iteration failed", "err", err.Error())
	}
}
