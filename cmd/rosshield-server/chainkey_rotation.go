package main

import (
	"crypto/ed25519"
	"errors"
	"fmt"

	"github.com/ssabro/rosshield/internal/domain/audit/keyrotation"
	"github.com/ssabro/rosshield/internal/platform/keystore"
	"github.com/ssabro/rosshield/internal/platform/metrics"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// chainkey_rotation.go — Phase 10.D-3+4 결선 어댑터.
//
// keyrotation.KeystoreHandleAllocator 와 keyrotation.Metrics 를 bootstrap KeyStore + metrics
// Registry 로 결선. P5 — keyrotation 도메인은 keystore/metrics 패키지를 직접 import 하지 않음.

// keyRotationMetricsAdapter 는 keyrotation.Metrics 를 metrics.Registry 로 결선합니다.
type keyRotationMetricsAdapter struct {
	reg *metrics.Registry
}

// IncRotation 는 keyrotation.Metrics 구현입니다.
func (a *keyRotationMetricsAdapter) IncRotation(status string) {
	if a == nil || a.reg == nil {
		return
	}
	a.reg.AuditRotationTotal.WithLabelValues(status).Inc()
}

// SetCurrentEpoch 는 keyrotation.Metrics 구현입니다.
func (a *keyRotationMetricsAdapter) SetCurrentEpoch(tenantID storage.TenantID, epoch int64) {
	if a == nil || a.reg == nil {
		return
	}
	a.reg.AuditKeyEpoch.WithLabelValues(string(tenantID)).Set(float64(epoch))
}

// chainKeyAllocator 는 keyrotation.KeystoreHandleAllocator 를 keystore + cfg 로 결선합니다.
//
// handle 명명 규칙: "audit-chain-{epoch}". file backend 는 keyHandle(cfg, name) 으로 전체 경로
// 산출, TPM backend 는 단순 식별자 그대로 사용 — keyHandle 함수가 분기 처리.
type chainKeyAllocator struct {
	ks  keystore.KeyStore
	cfg Config
}

// newChainKeyAllocator 는 chainKeyAllocator 를 생성하는 헬퍼입니다.
func newChainKeyAllocator(ks keystore.KeyStore, cfg Config) keyrotation.KeystoreHandleAllocator {
	return &chainKeyAllocator{ks: ks, cfg: cfg}
}

// AllocateForEpoch 는 keyrotation.KeystoreHandleAllocator 구현입니다.
//
// keystore handle 명명: "audit-chain-{epoch}". 같은 epoch 로 두 번 호출되면 같은 key 반환
// (LoadOrCreatePrivateKey idempotent 보장 — 본 round 는 epoch 단조 증가 가정 하 항상 신규).
func (a *chainKeyAllocator) AllocateForEpoch(newEpoch int64) (string, ed25519.PrivateKey, error) {
	if a == nil || a.ks == nil {
		return "", nil, errors.New("chainKeyAllocator: keystore not configured")
	}
	name := fmt.Sprintf("audit-chain-%d", newEpoch)
	handle := keyHandle(a.cfg, name)
	priv, err := a.ks.LoadOrCreatePrivateKey(handle)
	if err != nil {
		return "", nil, fmt.Errorf("chainKeyAllocator: load epoch=%d: %w", newEpoch, err)
	}
	return handle, priv, nil
}
