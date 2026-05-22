package main

// Phase 11.C-3 — audit chain hash version transition marker bootstrap hook.
//
// 본 파일은 bootstrap 에서 audit.EnsureHashVersionTransition 을 single Tx 안에서 호출하는
// glue layer 입니다. CLAUDE.md 정책 일관 — bootstrap.go 가 이미 2200 line 초과라 별 파일로
// 격리. 도메인 경계 일관 (audit 도메인은 transition emit 로직만 — orchestration 은 본 파일).

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/metrics"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// ensureHashVersionTransition 은 audit chain 의 hash version transition marker entry 가
// systemTenant 에 존재함을 보장합니다 (Phase 11.C-3).
//
// 동작:
//  1. tenant-scoped Tx 시작.
//  2. audit.EnsureHashVersionTransition 호출 — idempotent emit.
//  3. emit 또는 cache 결과를 logger 에 기록.
//
// idempotent 보장:
//   - 이미 transition entry 가 존재 (서버 재시작 등) → emit 0 + transition seq 만 Repo 에 cache.
//   - 미존재 → 1회 emit + transition seq cache.
//
// 본 함수가 실패하면 bootstrap fatal — audit chain hash version 분기가 비활성 상태로 부팅
// 되면 외부 검증 도구가 v1/v3 경계를 인식할 수 없어 audit chain 무결성 검증 회의 가능.
func ensureHashVersionTransition(
	ctx context.Context,
	store storage.Storage,
	svc audit.Service,
	repo interface {
		audit.HashVersionLocator
		audit.HashVersionTransitionSetter
	},
	tenantID storage.TenantID,
	logger *slog.Logger,
) error {
	if store == nil || svc == nil || repo == nil {
		return errors.New("bootstrap: ensureHashVersionTransition: all dependencies required")
	}
	if tenantID == "" {
		return errors.New("bootstrap: ensureHashVersionTransition: tenantID required")
	}

	ctx = storage.WithTenantID(ctx, tenantID)
	return store.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		entry, emitted, err := audit.EnsureHashVersionTransition(ctx, tx, svc, repo, repo, tenantID)
		if err != nil {
			return err
		}
		if emitted {
			logger.Info("audit chain hash version transition marker emitted",
				"tenant", string(tenantID), "seq", entry.Seq, "action", entry.Action)
		} else {
			logger.Debug("audit chain hash version transition marker exists (idempotent skip)",
				"tenant", string(tenantID), "seq", entry.Seq)
		}
		return nil
	})
}

// recordAuditChainHashVersion 은 transition emit (또는 cache) 후 audit_chain_hash_version
// gauge 를 활성 version (1 또는 3) 으로 set 하고 transition_total counter 를 1 증가시킵니다.
//
// emit / cache 모두 동일 — 본 process 가 transition 을 인식한 시점 자체가 metric trigger
// (counter 는 process scope 라 재부팅 후 0 부터 시작; 정상 운영 1).
//
// reg 가 nil 이면 noop — test fixture / minimal config.
func recordAuditChainHashVersion(reg *metrics.Registry, tenantID storage.TenantID) {
	if reg == nil {
		return
	}
	label := string(tenantID)
	reg.AuditChainHashVersion.WithLabelValues(label).Set(3)
	reg.AuditChainHashVersionTransitionTotal.WithLabelValues(label).Inc()
}
