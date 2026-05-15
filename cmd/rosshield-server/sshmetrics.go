package main

import (
	"time"

	"github.com/ssabro/rosshield/internal/platform/metrics"
)

// sshmetrics.go — sshpool ↔ metrics 어댑터 (scanrun SSH 통합 Stage 4).
//
// sshpool.ExecMetrics + sshpool.PoolMetrics 인터페이스를 metrics.Registry로 결선.
// P5 — sshpool 패키지가 metrics 패키지를 직접 import하지 않도록 어댑터 패턴 사용.

// sshExecMetricsAdapter는 sshpool.ExecMetrics를 metrics.Registry로 결선합니다.
type sshExecMetricsAdapter struct {
	reg *metrics.Registry
}

// ObserveExec는 sshpool.ExecMetrics 구현입니다.
//
// outcome = "success" | "error" | "timeout" — counter 증가 + duration histogram 기록.
func (a *sshExecMetricsAdapter) ObserveExec(outcome string, duration time.Duration) {
	if a == nil || a.reg == nil {
		return
	}
	a.reg.SSHExecTotal.WithLabelValues(outcome).Inc()
	a.reg.SSHExecDuration.WithLabelValues(outcome).Observe(duration.Seconds())
}

// sshPoolMetricsAdapter는 sshpool.PoolMetrics를 metrics.Registry로 결선합니다.
//
// 본 어댑터는 향후 Pool 결선(Stage 5와 통합) 시 사용됩니다 — 본 commit은 Stage 4
// 인프라 마감으로 ExecMetrics만 결선, PoolMetrics는 정의만(컴파일 타임 검증).
type sshPoolMetricsAdapter struct {
	reg *metrics.Registry
}

// IncDial는 sshpool.PoolMetrics 구현입니다.
func (a *sshPoolMetricsAdapter) IncDial(result string) {
	if a == nil || a.reg == nil {
		return
	}
	a.reg.SSHDialTotal.WithLabelValues(result).Inc()
}

// SetIdleConns는 sshpool.PoolMetrics 구현입니다.
func (a *sshPoolMetricsAdapter) SetIdleConns(n int) {
	if a == nil || a.reg == nil {
		return
	}
	a.reg.SSHIdleConnsGauge.Set(float64(n))
}
