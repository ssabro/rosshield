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

// DefaultInterval은 polling 간격 기본값입니다.
const DefaultInterval = 30 * time.Second

// Deps는 Collector 의존성입니다.
type Deps struct {
	Querier  Querier           // primary PG querier — pg_stat_replication 조회 (보통 pgxpool.Pool)
	Registry *metrics.Registry // ReplicationLagSeconds Gauge emit 대상
	Interval time.Duration     // 0이면 DefaultInterval (30s)
	Logger   *slog.Logger
}

// Collector는 pg_stat_replication을 polling해 lag metric을 emit합니다.
type Collector struct {
	deps   Deps
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
	return &Collector{
		deps:   deps,
		closed: make(chan struct{}),
	}, nil
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
// 조회 실패는 logger.Warn만 — 부분 실패가 collector를 중단시키지 않음.
//
// stale label 처리: 매 polling 시작 시 Gauge.Reset() — 사라진 subscriber label 자동
// 제거 + 등장한 subscriber만 다시 Set. 30초 간격 polling이라 cardinality 영향 미미.
func (c *Collector) pollOnce(ctx context.Context) {
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
