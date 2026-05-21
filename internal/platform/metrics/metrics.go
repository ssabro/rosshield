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
	dto "github.com/prometheus/client_model/go"
)

// Registry는 rosshield-scoped Prometheus registry입니다.
//
// global default registry 사용을 피해 테스트 결정성·격리 강화. ProcessCollector + GoCollector도 직접 등록.
type Registry struct {
	reg *prometheus.Registry

	// === 핵심 counter (label: tenant) ===

	ScansStartedTotal        *prometheus.CounterVec
	ScansCompletedTotal      *prometheus.CounterVec // label: tenant, status (completed|failed|cancelled) — usage 통계
	ScanFailedChecksTotal    *prometheus.CounterVec // label: tenant — completed scan별 failed check 누적 (violation rate 산정)
	WebhookDeliveriesTotal   *prometheus.CounterVec // label: status (success|failed|dead)
	InvitationsSentTotal     *prometheus.CounterVec
	InvitationsAcceptedTotal *prometheus.CounterVec

	// === audit chain anchor (label: tenant) ===

	AuditChainHeadSeq *prometheus.GaugeVec

	// === audit chain key rotation (Phase 10.D-3+4) ===
	//
	// AuditRotationTotal: rotation 호출 결과 누적, label status=success|failed|skipped.
	// AuditKeyEpoch: tenant 별 현재 활성 epoch (audit_chain_keys 의 활성 row epoch).
	AuditRotationTotal *prometheus.CounterVec
	AuditKeyEpoch      *prometheus.GaugeVec

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

	// === Multi-region replication lag (Phase 8 MR.T8 — v0.7.x carryover 일소) ===
	//
	// ReplicationLagSeconds: primary 측에서 pg_stat_replication.replay_lag을 polling해
	// EPOCH(초)로 노출. label=application_name으로 multi-replica 대응. follower 또는
	// sqlite storage에서는 emit 안 함 (collector 미시작).
	ReplicationLagSeconds *prometheus.GaugeVec

	// === SSH pool (scanrun SSH 통합 Stage 4) ===
	//
	// SSHExecTotal: SSH exec 호출 누적, label outcome=success|error|timeout.
	// SSHExecDuration: SSH exec 응답 시간 histogram, label outcome.
	// SSHDialTotal: dial 시도 누적, label result=ok|fail.
	// SSHIdleConnsGauge: 현재 idle pool 안 conn 수 (모든 PoolKey 합).
	SSHExecTotal      *prometheus.CounterVec
	SSHExecDuration   *prometheus.HistogramVec
	SSHDialTotal      *prometheus.CounterVec
	SSHIdleConnsGauge prometheus.Gauge
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

	r.ScansCompletedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "rosshield",
		Subsystem: "scan",
		Name:      "completed_total",
		Help:      "Number of scan sessions reaching terminal state, partitioned by tenant and status (completed|failed|cancelled). Usage 통계 + sales pitch 자료.",
	}, []string{"tenant", "status"})

	r.ScanFailedChecksTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "rosshield",
		Subsystem: "scan",
		Name:      "failed_checks_total",
		Help:      "Cumulative number of failed checks across all completed scans, partitioned by tenant. Used as violation rate signal (alongside scan_completed_total).",
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

	r.AuditRotationTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "rosshield",
		Subsystem: "audit",
		Name:      "rotation_total",
		Help:      "Cumulative audit chain signer key rotation invocations, partitioned by status (success|failed|skipped).",
	}, []string{"status"})

	r.AuditKeyEpoch = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "rosshield",
		Subsystem: "audit",
		Name:      "key_epoch",
		Help:      "Current active audit chain signer key epoch, partitioned by tenant.",
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

	r.ReplicationLagSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "rosshield",
		Subsystem: "replication",
		Name:      "lag_seconds",
		Help:      "PG logical replication lag in seconds, partitioned by subscriber application_name. Emitted only on primary instances when replication is enabled.",
	}, []string{"application_name"})

	r.SSHExecTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "rosshield",
		Subsystem: "ssh",
		Name:      "exec_total",
		Help:      "Cumulative SSH exec calls, partitioned by outcome (success|error|timeout).",
	}, []string{"outcome"})

	r.SSHExecDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "rosshield",
		Subsystem: "ssh",
		Name:      "exec_duration_seconds",
		Help:      "Duration of SSH exec calls, partitioned by outcome.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"outcome"})

	r.SSHDialTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "rosshield",
		Subsystem: "ssh",
		Name:      "dial_total",
		Help:      "Cumulative SSH dial attempts, partitioned by result (ok|fail).",
	}, []string{"result"})

	r.SSHIdleConnsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "rosshield",
		Subsystem: "ssh",
		Name:      "idle_conns",
		Help:      "Current number of idle pooled SSH connections (sum across all PoolKeys).",
	})

	reg.MustRegister(
		r.ScansStartedTotal,
		r.ScansCompletedTotal,
		r.ScanFailedChecksTotal,
		r.WebhookDeliveriesTotal,
		r.InvitationsSentTotal,
		r.InvitationsAcceptedTotal,
		r.AuditChainHeadSeq,
		r.AuditRotationTotal,
		r.AuditKeyEpoch,
		r.EventPublishDuration,
		r.HARole,
		r.HALeaderEpoch,
		r.HAFailoverTotal,
		r.ReplicationLagSeconds,
		r.SSHExecTotal,
		r.SSHExecDuration,
		r.SSHDialTotal,
		r.SSHIdleConnsGauge,
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

// TenantUsage는 한 tenant의 누적 사용 통계입니다 (E38 onboarding/billing 자료).
//
// 본 구조는 handlers/usage_stats.go의 JSON 응답 source. Prometheus dto 추상화 차단 —
// handler는 prometheus 패키지를 import하지 않고, 본 helper만 호출.
type TenantUsage struct {
	ScansStarted     float64            // rosshield_scan_started_total{tenant}
	ScansCompleted   map[string]float64 // status (completed|failed|cancelled) → 카운트
	ScanFailedChecks float64            // rosshield_scan_failed_checks_total{tenant} 누적 violation
}

// GetTenantUsage는 주어진 tenant의 사용 통계 카운트를 반환합니다.
//
// 첫 호출 시 metric series가 없으면 0 series가 자동 생성되지만 무해 (Prometheus 표준).
// counter는 process restart 시 0부터 다시 카운트 — 본 helper는 정확한 누적이 아닌 process
// scope 카운트를 반환. 정확한 누적은 외부 Prometheus + Grafana로 구현.
func (r *Registry) GetTenantUsage(tenantID string) TenantUsage {
	out := TenantUsage{
		ScansCompleted: map[string]float64{},
	}
	out.ScansStarted = readCounterValue(r.ScansStartedTotal.WithLabelValues(tenantID))
	for _, status := range []string{"completed", "failed", "cancelled"} {
		out.ScansCompleted[status] = readCounterValue(r.ScansCompletedTotal.WithLabelValues(tenantID, status))
	}
	out.ScanFailedChecks = readCounterValue(r.ScanFailedChecksTotal.WithLabelValues(tenantID))
	return out
}

// readCounterValue는 prometheus.Counter의 현재 값을 반환합니다 (dto 직접 노출 회피).
func readCounterValue(c prometheus.Counter) float64 {
	m := &dto.Metric{}
	if err := c.Write(m); err != nil {
		return 0
	}
	return m.Counter.GetValue()
}
