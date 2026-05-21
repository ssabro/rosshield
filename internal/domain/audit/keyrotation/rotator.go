// Package keyrotation 은 audit chain signer key 자동 rotation 의 L3 Application
// Service 입니다 (Phase 10.D-3 + 10.D-4).
//
// 설계: docs/design/notes/audit-chain-rotation-automation-design.md §6.3 + §6.4 + §12.1.
//
// 책임:
//   - 새 Ed25519 key 생성 + keystore handle 영속.
//   - self-sign / verify round-trip (fail-safe — 검증 통과 시에만 swap).
//   - audit_chain_keys append (새 epoch) + 이전 epoch revoke + audit.chain.key_rotated emit.
//     모두 단일 storage.Tx 안 — partial failure 시 rollback.
//   - SwappableSigner.Swap (RWMutex queue 패턴).
//   - leader-only gate (HA RoleProvider) + min interval idempotency.
//
// 도메인 경계:
//   - keyrotation 은 audit·signer·keystore 추상만 의존. ha·scheduler·metrics 등 platform
//     레이어는 본 패키지를 호출하는 어댑터(scheduler job) 측에서 결선.
package keyrotation

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/clock"
	"github.com/ssabro/rosshield/internal/platform/signer"
	"github.com/ssabro/rosshield/internal/platform/signer/soft"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// DefaultMinInterval 은 동일 epoch 가 단조 증가하지 못할 정도로 짧은 호출을 차단하는 idempotency
// 가드입니다. 본 값보다 빠른 재호출은 noop (ErrTooSoon).
//
// Quarterly cron (D-P10D-2 = 90일) baseline 대비 충분히 작은 1시간. test 에서는 0 으로 disable.
const DefaultMinInterval = 1 * time.Hour

// KeystoreHandleAllocator 는 새 epoch 의 keystore handle 을 할당하고 raw ed25519 private
// key 를 영속 + 반환합니다.
//
// bootstrap 어댑터가 keystore.KeyStore + keyHandle(cfg, "audit-chain-N") 합성을 본 callback
// 으로 wrap. 도메인은 keystore 패키지를 직접 import 하지 않음 (의존 가드).
type KeystoreHandleAllocator interface {
	// AllocateForEpoch 는 newEpoch 에 대한 새 keystore handle 을 생성·영속하고 raw ed25519
	// private key 를 반환합니다.
	//
	// 반환값:
	//   - handle: keystore 안의 식별자 (file path 또는 TPM object).
	//   - privateKey: 새 ed25519 private key.
	//   - err: 실패 시 rotation 전체 abort (audit_chain_keys row 미커밋).
	AllocateForEpoch(newEpoch int64) (handle string, privateKey ed25519.PrivateKey, err error)
}

// AllocatorFunc 는 함수를 KeystoreHandleAllocator 로 어댑팅합니다 (bootstrap 일회용 클로저용).
type AllocatorFunc func(newEpoch int64) (string, ed25519.PrivateKey, error)

// AllocateForEpoch 는 KeystoreHandleAllocator interface 구현입니다.
func (f AllocatorFunc) AllocateForEpoch(newEpoch int64) (string, ed25519.PrivateKey, error) {
	return f(newEpoch)
}

// LeaderProvider 는 HA leader 여부를 질의하는 minimal interface 입니다.
//
// ha.Manager 가 자동 만족 (duck typing). nil 가능 — HA 비활성 시 통과.
type LeaderProvider interface {
	IsLeader() bool
}

// Metrics 는 rotation 성공·실패·skip 카운터 인터페이스입니다.
//
// platform/metrics.Registry 가 어댑터 패턴으로 구현. nil 가능 — emit 생략.
type Metrics interface {
	// IncRotation 는 status="success"|"failed"|"skipped" 로 counter 증가.
	IncRotation(status string)
	// SetCurrentEpoch 는 활성 epoch gauge 갱신 (tenant scope).
	SetCurrentEpoch(tenantID storage.TenantID, epoch int64)
}

// Deps 는 KeyRotator 의존성입니다. 모두 필수 (Metrics 와 Leader 만 옵션).
type Deps struct {
	Storage     storage.Storage
	Audit       audit.Service
	ChainKeys   audit.ChainKeyRepository
	Signer      *signer.SwappableSigner
	Allocator   KeystoreHandleAllocator
	Clock       clock.Clock
	Logger      *slog.Logger
	Metrics     Metrics          // 옵션 — nil 시 emit 생략.
	Leader      LeaderProvider   // 옵션 — nil 시 HA 비활성 가정.
	MinInterval time.Duration    // 0 이면 DefaultMinInterval.
	TenantID    storage.TenantID // 현 단일 system tenant 전제 — 멀티테넌시 확장 시 별 round.
}

// KeyRotator 는 audit chain signer key rotation 의 단일 orchestrator 입니다.
type KeyRotator struct {
	deps Deps

	mu          sync.Mutex // 동시 RotateNow 직렬화 — scheduler + manual API 충돌 방지.
	lastRotated time.Time  // last successful rotation 시각 — min interval 가드.
}

// 공통 에러.
var (
	ErrNotLeader            = errors.New("keyrotation: instance is not leader")
	ErrTooSoon              = errors.New("keyrotation: rotation skipped (min interval not reached)")
	ErrVerifyRoundtripFail  = errors.New("keyrotation: new key self-verify failed")
	ErrAllocatorReturnedNil = errors.New("keyrotation: allocator returned nil private key")
)

// Trigger 는 rotation 호출 출처를 식별합니다 (audit emit 본문에 기록).
const (
	TriggerScheduler = "scheduler"
	TriggerManual    = "manual"
	TriggerCLI       = "cli"
)

// New 는 KeyRotator 를 만듭니다. 필수 deps 누락 시 error.
func New(deps Deps) (*KeyRotator, error) {
	if deps.Storage == nil {
		return nil, fmt.Errorf("keyrotation: Storage required")
	}
	if deps.Audit == nil {
		return nil, fmt.Errorf("keyrotation: Audit required")
	}
	if deps.ChainKeys == nil {
		return nil, fmt.Errorf("keyrotation: ChainKeys required")
	}
	if deps.Signer == nil {
		return nil, fmt.Errorf("keyrotation: Signer required")
	}
	if deps.Allocator == nil {
		return nil, fmt.Errorf("keyrotation: Allocator required")
	}
	if deps.Clock == nil {
		return nil, fmt.Errorf("keyrotation: Clock required")
	}
	if deps.Logger == nil {
		return nil, fmt.Errorf("keyrotation: Logger required")
	}
	if deps.TenantID == "" {
		return nil, fmt.Errorf("keyrotation: TenantID required")
	}
	// MinInterval=0 은 "disable guard" 의미 (test 결정성). 음수면 default.
	if deps.MinInterval < 0 {
		deps.MinInterval = DefaultMinInterval
	}
	return &KeyRotator{deps: deps}, nil
}

// RotateNow 는 단일 rotation 을 수행합니다. idempotent — leader 아님 / 최근 rotation /
// 동시 호출 시 noop (각각 sentinel error 반환).
//
// 흐름:
//  1. leader gate. follower 면 ErrNotLeader.
//  2. min interval 가드 — 마지막 rotation 후 < MinInterval 이면 ErrTooSoon.
//  3. Allocator 가 새 key 생성 + keystore 영속.
//  4. self-sign + verify round-trip.
//  5. 단일 storage.Tx 안에서: current epoch 조회 → new epoch 결정 → AppendChainKeyEpoch
//     → 이전 epoch RevokeChainKeyEpoch → audit.chain.key_rotated emit → commit.
//  6. Tx commit 성공 시 SwappableSigner.Swap (실패 시 rollback + new signer 미반영).
//  7. metrics + log emit.
func (r *KeyRotator) RotateNow(ctx context.Context, trigger string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.deps.Leader != nil && !r.deps.Leader.IsLeader() {
		r.recordMetric("skipped")
		return ErrNotLeader
	}

	now := r.deps.Clock.Now().UTC()
	if r.deps.MinInterval > 0 && !r.lastRotated.IsZero() && now.Sub(r.lastRotated) < r.deps.MinInterval {
		r.recordMetric("skipped")
		return ErrTooSoon
	}

	// 1) current epoch 조회 — Tx 외부에서 read-only (allocator 가 newEpoch 받아야 하므로).
	var currentEpoch int64
	tenantCtx := storage.WithTenantID(ctx, r.deps.TenantID)
	readErr := r.deps.Storage.Tx(tenantCtx, func(c context.Context, tx storage.Tx) error {
		ce, err := r.deps.ChainKeys.CurrentChainKeyEpoch(c, tx, r.deps.TenantID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				// 활성 epoch 없음 — bootstrap row revoke 된 비정상 상태. 보수적 처리 위해 wrapper epoch 사용.
				currentEpoch = r.deps.Signer.CurrentEpoch()
				return nil
			}
			return fmt.Errorf("read current epoch: %w", err)
		}
		currentEpoch = ce.Epoch
		return nil
	})
	if readErr != nil {
		r.recordMetric("failed")
		return fmt.Errorf("keyrotation: read current epoch: %w", readErr)
	}

	newEpoch := currentEpoch + 1
	if newEpoch <= 0 {
		newEpoch = 1
	}

	// 2) Allocator 가 새 key 생성 + keystore 영속.
	handle, priv, err := r.deps.Allocator.AllocateForEpoch(newEpoch)
	if err != nil {
		r.recordMetric("failed")
		return fmt.Errorf("keyrotation: allocator: %w", err)
	}
	if len(priv) != ed25519.PrivateKeySize {
		r.recordMetric("failed")
		return ErrAllocatorReturnedNil
	}

	newSigner := soft.WrapPrivateKey(priv)

	// 3) self-sign + verify round-trip — fail-safe (R6 일관).
	probe := []byte("keyrotation-self-test")
	probeSig, _, err := newSigner.Sign(probe)
	if err != nil {
		r.recordMetric("failed")
		return fmt.Errorf("keyrotation: self-sign: %w", err)
	}
	if err := newSigner.Verify(probe, probeSig); err != nil {
		r.recordMetric("failed")
		return fmt.Errorf("%w: %v", ErrVerifyRoundtripFail, err)
	}

	pubHex := hex.EncodeToString(newSigner.PublicKey())
	newKeyID := newSigner.KeyID()

	// 4) 단일 Tx 안에서 chain_keys append + revoke + audit emit.
	commitErr := r.deps.Storage.Tx(tenantCtx, func(c context.Context, tx storage.Tx) error {
		ep := audit.ChainKeyEpoch{
			Epoch:          newEpoch,
			TenantID:       r.deps.TenantID,
			KeyID:          newKeyID,
			PublicKeyHex:   pubHex,
			KeystoreHandle: handle,
			CreatedAt:      now,
			CreatedBy:      normalizeTrigger(trigger),
			AuditEntrySeq:  0, // 채워질 entry seq 는 audit emit 후 결정 — 본 round 는 0 으로 두고 후속 round 에서 audit_chain_keys 의 entry seq 갱신 결선 가능.
		}
		assigned, err := r.deps.ChainKeys.AppendChainKeyEpoch(c, tx, ep)
		if err != nil {
			return fmt.Errorf("append chain key epoch: %w", err)
		}
		if assigned != newEpoch {
			newEpoch = assigned // backend 가 다른 epoch 를 할당한 경우 일치시킴.
		}

		// 이전 epoch revoke (있는 경우만).
		if currentEpoch > 0 && currentEpoch != newEpoch {
			if err := r.deps.ChainKeys.RevokeChainKeyEpoch(c, tx, r.deps.TenantID, currentEpoch, now); err != nil {
				if !errors.Is(err, audit.ErrChainKeyAlreadyRevoked) && !errors.Is(err, storage.ErrNotFound) {
					return fmt.Errorf("revoke prior epoch %d: %w", currentEpoch, err)
				}
			}
		}

		// audit.chain.key_rotated emit — 본 Append 는 아직 epoch=currentEpoch (SwappableSigner
		// 가 swap 되기 전) 으로 기록됨. 다음 entry 부터 newEpoch.
		payload := fmt.Sprintf(
			`{"fromEpoch":%d,"toEpoch":%d,"newKeyId":%q,"publicKeyHex":%q,"trigger":%q}`,
			currentEpoch, newEpoch, newKeyID, pubHex, normalizeTrigger(trigger))
		_, err = r.deps.Audit.Append(c, tx, audit.AppendRequest{
			TenantID: r.deps.TenantID,
			Actor:    audit.Actor{Type: audit.ActorSystem, ID: "system"},
			Action:   "audit.chain.key_rotated",
			Target:   audit.Target{Type: "audit_chain", ID: fmt.Sprintf("epoch:%d", newEpoch)},
			Payload:  []byte(payload),
			Outcome:  audit.OutcomeSuccess,
		})
		if err != nil {
			return fmt.Errorf("audit emit: %w", err)
		}
		return nil
	})
	if commitErr != nil {
		r.recordMetric("failed")
		return fmt.Errorf("keyrotation: commit: %w", commitErr)
	}

	// 5) commit 후 SwappableSigner hot-swap (Tx 외부 — Tx 실패 시 swap 안 함).
	r.deps.Signer.Swap(newSigner, newEpoch)
	r.lastRotated = now
	r.recordMetric("success")
	r.recordEpochGauge(newEpoch)
	r.deps.Logger.Info("audit chain key rotated",
		"fromEpoch", currentEpoch, "toEpoch", newEpoch,
		"newKeyId", newKeyID, "trigger", normalizeTrigger(trigger),
		"handle", handle)
	return nil
}

// LastRotatedAt 은 마지막 성공 rotation 시각을 반환합니다 (test/inspection 용).
func (r *KeyRotator) LastRotatedAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastRotated
}

func (r *KeyRotator) recordMetric(status string) {
	if r.deps.Metrics == nil {
		return
	}
	r.deps.Metrics.IncRotation(status)
}

func (r *KeyRotator) recordEpochGauge(epoch int64) {
	if r.deps.Metrics == nil {
		return
	}
	r.deps.Metrics.SetCurrentEpoch(r.deps.TenantID, epoch)
}

func normalizeTrigger(t string) string {
	switch t {
	case TriggerScheduler, TriggerManual, TriggerCLI:
		return t
	case "":
		return TriggerManual
	default:
		return t
	}
}
