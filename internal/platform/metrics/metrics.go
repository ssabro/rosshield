// Package metrics는 rosshield의 Prometheus exposition 결선입니다 (E27 Phase 4).
//
// 책임:
//
//   - 핵심 도메인 metric 정의 (scans·webhook·audit·event publish duration).
//   - Registry 격리 — global default registry 오염 방지(테스트 결정성).
//   - HTTP handler 노출 (`promhttp.HandlerFor`).
//
// 도메인 결합 (P5):
//
//	본 패키지는 도메인을 import 안 함. EventBus 구독 어댑터(`eventbridge.go`)가
//	도메인 이벤트를 받아 counter 증가. 도메인 service는 metric 의존 0.
//
// 옵트인:
//
//	--metrics-addr 플래그가 비어 있으면 endpoint mount X — production 외부 노출 신중.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry는 rosshield-scoped Prometheus registry입니다.
//
// global default registry 사용을 피해 테스트 결정성·격리 강화. ProcessCollector + GoCollector도 직접 등록.
type Registry struct {
	reg *prometheus.Registry

	// === 핵심 counter (label: tenant) ===

	ScansStartedTotal        *prometheus.CounterVec
	WebhookDeliveriesTotal   *prometheus.CounterVec // label: status (success|failed|dead)
	InvitationsSentTotal     *prometheus.CounterVec
	InvitationsAcceptedTotal *prometheus.CounterVec

	// === audit chain anchor (label: tenant) ===

	AuditChainHeadSeq *prometheus.GaugeVec

	// === histogram ===

	EventPublishDuration *prometheus.HistogramVec // label: topic

	// === HA leader-election (E25 Stage 4 잔여, R30-2 PG advisory lock) ===
	//
	// HARole: 0=follower, 1=leader. HAEnabled=false 시 emit 안 함 (gauge 부재).
	// HALeaderEpoch: 현재 보유 fence token (PG sequence nextval). follower면 0.
	// HAFailoverTotal: 누적 leader 승격 횟수 (재부팅 후 0부터 시작 — process scope).
	HARole          prometheus.Gauge
	HALeaderEpoch   prometheus.Gauge
	HAFailoverTotal prometheus.Counter
}

// New는 새 Registry를 만듭니다.
//
// process·go runtime collectors 도 자동 등록 (Prometheus 표준).
func New() *Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)

	r := &Registry{reg: reg}

	r.ScansStartedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "rosshield",
		Subsystem: "scan",
		Name:      "started_total",
		Help:      "Number of scan sessions started, partitioned by tenant.",
	}, []string{"tenant"})

	r.WebhookDeliveriesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "rosshield",
		Subsystem: "webhook",
		Name:      "deliveries_total",
		Help:      "Number of webhook delivery attempts, partitioned by status (success|failed|dead).",
	}, []string{"status"})

	r.InvitationsSentTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "rosshield",
		Subsystem: "invitation",
		Name:      "sent_total",
		Help:      "Number of user invitations sent, partitioned by tenant.",
	}, []string{"tenant"})

	r.InvitationsAcceptedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "rosshield",
		Subsystem: "invitation",
		Name:      "accepted_total",
		Help:      "Number of user invitations accepted, partitioned by tenant.",
	}, []string{"tenant"})

	r.AuditChainHeadSeq = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "rosshield",
		Subsystem: "audit",
		Name:      "chain_head_seq",
		Help:      "Current head sequence of the audit hash chain, partitioned by tenant.",
	}, []string{"tenant"})

	r.EventPublishDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "rosshield",
		Subsystem: "event",
		Name:      "publish_duration_seconds",
		Help:      "Duration of EventBus publish calls, partitioned by topic.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"topic"})

	r.HARole = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "rosshield",
		Subsystem: "ha",
		Name:      "role",
		Help:      "Current HA role of this instance (0=follower, 1=leader). Emitted only when --ha-enabled.",
	})

	r.HALeaderEpoch = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "rosshield",
		Subsystem: "ha",
		Name:      "leader_epoch",
		Help:      "Current leader epoch (fence token from leader_epoch_seq). 0 when this instance is follower.",
	})

	r.HAFailoverTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "rosshield",
		Subsystem: "ha",
		Name:      "failover_total",
		Help:      "Cumulative number of leader promotions on this instance (process scope, resets on restart).",
	})

	reg.MustRegister(
		r.ScansStartedTotal,
		r.WebhookDeliveriesTotal,
		r.InvitationsSentTotal,
		r.InvitationsAcceptedTotal,
		r.AuditChainHeadSeq,
		r.EventPublishDuration,
		r.HARole,
		r.HALeaderEpoch,
		r.HAFailoverTotal,
	)

	return r
}

// Handler는 promhttp HTTP handler를 반환합니다 (`/metrics` mount용).
//
// 옵션 — error는 stderr만, 압축은 클라이언트(scraper)가 결정.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{
		EnableOpenMetrics: false,
	})
}

// PrometheusRegistry는 underlying *prometheus.Registry를 노출합니다 (eventbridge·테스트용).
//
// 도메인 코드는 사용 금지 — 본 패키지의 typed counter 메서드만 호출.
func (r *Registry) PrometheusRegistry() *prometheus.Registry {
	return r.reg
}
