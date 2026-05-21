package audit

import (
	"context"
	"errors"
	"time"

	"github.com/ssabro/rosshield/internal/platform/storage"
)

// ChainKeyEpoch는 audit chain signer key rotation epoch 1행입니다.
//
// design: docs/design/notes/audit-chain-rotation-automation-design.md §8.1.
//
// 의미:
//   - Epoch 는 tenant 별 단조 증가 (1부터 시작). 마이그레이션 0037 bootstrap row 가 epoch=1.
//   - KeyID 는 Ed25519 KeyID ("key_" + hex(sha256(pub)[:8])).
//   - PublicKeyHex 는 Ed25519 public key 32B 의 hex encoding (외부 검증 도구가 사용).
//   - KeystoreHandle 은 file path 또는 TPM handle.
//   - CreatedAt 은 epoch 생성 시각. RevokedAt 은 rotation 후 이전 key 폐기 시각 (nullable).
//   - CreatedBy 는 actor (scheduler·admin·cli·migration).
//   - AuditEntrySeq 는 rotation event 의 audit entry seq (epoch=1 bootstrap 은 0).
type ChainKeyEpoch struct {
	Epoch          int64
	TenantID       storage.TenantID
	KeyID          string
	PublicKeyHex   string
	KeystoreHandle string
	CreatedAt      time.Time
	RevokedAt      *time.Time
	CreatedBy      string
	AuditEntrySeq  int64
}

// IsRevoked 는 revoked_at 가 채워졌는지 여부입니다.
func (e ChainKeyEpoch) IsRevoked() bool {
	return e.RevokedAt != nil
}

// ChainKeyRepository 는 audit_chain_keys 테이블 접근 진입점입니다.
//
// 모든 메서드는 외부 트랜잭션을 받아 도메인 변경과 같은 Tx 에 묶일 수 있게 합니다 (P5).
// signer hot-swap 등의 별 stage 가 본 인터페이스를 통해 epoch 보존을 수행합니다.
type ChainKeyRepository interface {
	// ListChainKeyEpochs 는 tenant 의 모든 epoch 를 epoch ASC 순으로 반환합니다.
	ListChainKeyEpochs(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) ([]ChainKeyEpoch, error)

	// CurrentChainKeyEpoch 는 tenant 의 활성(revoked_at IS NULL) epoch 를 반환합니다.
	// 여러 row 가 활성인 경우(이론상 불가) epoch DESC 첫 row 를 반환합니다.
	// 활성 epoch 가 없으면 storage.ErrNotFound 반환.
	CurrentChainKeyEpoch(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) (*ChainKeyEpoch, error)

	// AppendChainKeyEpoch 는 새 epoch row 를 insert 합니다.
	// epoch.Epoch 가 0 이면 storage backend 의 autoincrement 가 할당. 비-0 이면 명시 epoch 사용.
	// 반환값은 할당된 epoch.
	AppendChainKeyEpoch(ctx context.Context, tx storage.Tx, epoch ChainKeyEpoch) (int64, error)

	// RevokeChainKeyEpoch 는 (tenant, epoch) 의 revoked_at 를 set 합니다.
	// 이미 revoke 된 row 는 ErrChainKeyAlreadyRevoked.
	// 존재하지 않는 (tenant, epoch) 는 storage.ErrNotFound.
	RevokeChainKeyEpoch(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, epoch int64, revokedAt time.Time) error
}

// ChainKeyRepository 공통 에러.
var (
	ErrChainKeyAlreadyRevoked = errors.New("audit: chain key epoch is already revoked")
	ErrChainKeyInvalidEpoch   = errors.New("audit: chain key epoch must be > 0 or zero for auto-assign")
)
