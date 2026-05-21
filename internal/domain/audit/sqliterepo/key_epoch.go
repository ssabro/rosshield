package sqliterepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// KeyEpochRepo는 audit.ChainKeyRepository의 SQLite/PG 호환 구현입니다.
//
// design: docs/design/notes/audit-chain-rotation-automation-design.md §8.1.
//
// PG stdlib bridge (storage/postgres/pg.go) 가 *sql.Tx 를 발행하므로 본 구현은 양 driver
// 에서 변경 없이 동작합니다 (E22-C 일관).
type KeyEpochRepo struct{}

// NewKeyEpochRepo는 새 KeyEpochRepo 를 반환합니다.
func NewKeyEpochRepo() *KeyEpochRepo {
	return &KeyEpochRepo{}
}

// 컴파일 시점 인터페이스 매칭 보증.
var _ audit.ChainKeyRepository = (*KeyEpochRepo)(nil)

// ListChainKeyEpochs 는 tenant 의 모든 epoch 를 epoch ASC 순으로 반환합니다.
func (r *KeyEpochRepo) ListChainKeyEpochs(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) ([]audit.ChainKeyEpoch, error) {
	rows, err := tx.Query(ctx, `
SELECT epoch, tenant_id, key_id, public_key_hex, keystore_handle,
       created_at, revoked_at, created_by, audit_entry_seq
  FROM audit_chain_keys
 WHERE tenant_id = ?
 ORDER BY epoch ASC`,
		string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("audit: list chain key epochs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []audit.ChainKeyEpoch
	for rows.Next() {
		e, err := scanKeyEpoch(rows, tenantID)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: list chain key epochs rows: %w", err)
	}
	return out, nil
}

// CurrentChainKeyEpoch 는 tenant 의 활성(revoked_at IS NULL) epoch 를 반환합니다.
func (r *KeyEpochRepo) CurrentChainKeyEpoch(ctx context.Context, tx storage.Tx, tenantID storage.TenantID) (*audit.ChainKeyEpoch, error) {
	row := tx.QueryRow(ctx, `
SELECT epoch, tenant_id, key_id, public_key_hex, keystore_handle,
       created_at, revoked_at, created_by, audit_entry_seq
  FROM audit_chain_keys
 WHERE tenant_id = ? AND revoked_at IS NULL
 ORDER BY epoch DESC
 LIMIT 1`,
		string(tenantID))

	e, err := scanKeyEpochRow(row, tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("audit: current chain key epoch: %w", err)
	}
	return &e, nil
}

// AppendChainKeyEpoch 는 새 epoch row 를 insert 합니다.
//
// epoch.Epoch == 0 이면 backend 의 autoincrement 가 할당. 비-0 이면 명시 epoch 사용.
// 반환값은 INSERT 후 SELECT 로 회수한 epoch.
func (r *KeyEpochRepo) AppendChainKeyEpoch(ctx context.Context, tx storage.Tx, ep audit.ChainKeyEpoch) (int64, error) {
	if ep.Epoch < 0 {
		return 0, audit.ErrChainKeyInvalidEpoch
	}
	if ep.TenantID == "" {
		return 0, fmt.Errorf("audit: ChainKeyEpoch.TenantID is required")
	}
	if ep.KeyID == "" || ep.PublicKeyHex == "" || ep.KeystoreHandle == "" || ep.CreatedBy == "" {
		return 0, fmt.Errorf("audit: ChainKeyEpoch required fields missing")
	}

	createdAt := ep.CreatedAt.UTC().Format(time.RFC3339Nano)
	var revokedAt sql.NullString
	if ep.RevokedAt != nil {
		revokedAt = sql.NullString{String: ep.RevokedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}

	if ep.Epoch == 0 {
		// autoincrement.
		_, err := tx.Exec(ctx, `
INSERT INTO audit_chain_keys
    (tenant_id, key_id, public_key_hex, keystore_handle,
     created_at, revoked_at, created_by, audit_entry_seq)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			string(ep.TenantID), ep.KeyID, ep.PublicKeyHex, ep.KeystoreHandle,
			createdAt, revokedAt, ep.CreatedBy, ep.AuditEntrySeq)
		if err != nil {
			return 0, fmt.Errorf("audit: append chain key epoch: %w", err)
		}
	} else {
		_, err := tx.Exec(ctx, `
INSERT INTO audit_chain_keys
    (epoch, tenant_id, key_id, public_key_hex, keystore_handle,
     created_at, revoked_at, created_by, audit_entry_seq)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ep.Epoch, string(ep.TenantID), ep.KeyID, ep.PublicKeyHex, ep.KeystoreHandle,
			createdAt, revokedAt, ep.CreatedBy, ep.AuditEntrySeq)
		if err != nil {
			return 0, fmt.Errorf("audit: append chain key epoch: %w", err)
		}
	}

	// INSERT 직후 epoch 회수: (tenant_id, key_id) UNIQUE 으로 안전 lookup.
	row := tx.QueryRow(ctx, `
SELECT epoch FROM audit_chain_keys WHERE tenant_id = ? AND key_id = ?`,
		string(ep.TenantID), ep.KeyID)
	var assigned int64
	if err := row.Scan(&assigned); err != nil {
		return 0, fmt.Errorf("audit: read assigned epoch: %w", err)
	}
	return assigned, nil
}

// RevokeChainKeyEpoch 는 (tenant, epoch) 의 revoked_at 를 set 합니다.
func (r *KeyEpochRepo) RevokeChainKeyEpoch(ctx context.Context, tx storage.Tx, tenantID storage.TenantID, epoch int64, revokedAt time.Time) error {
	// 사전 SELECT 로 존재 + 미revoke 여부 확인. revoked_at IS NOT NULL 이면 ErrChainKeyAlreadyRevoked.
	row := tx.QueryRow(ctx, `
SELECT revoked_at FROM audit_chain_keys WHERE tenant_id = ? AND epoch = ?`,
		string(tenantID), epoch)
	var existing sql.NullString
	if err := row.Scan(&existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.ErrNotFound
		}
		return fmt.Errorf("audit: read chain key epoch for revoke: %w", err)
	}
	if existing.Valid {
		return audit.ErrChainKeyAlreadyRevoked
	}

	revokedStr := revokedAt.UTC().Format(time.RFC3339Nano)
	_, err := tx.Exec(ctx, `
UPDATE audit_chain_keys
   SET revoked_at = ?
 WHERE tenant_id = ? AND epoch = ?`,
		revokedStr, string(tenantID), epoch)
	if err != nil {
		return fmt.Errorf("audit: revoke chain key epoch: %w", err)
	}
	return nil
}

// scanKeyEpoch 는 audit_chain_keys 한 row(*sql.Rows)를 ChainKeyEpoch 로 변환합니다.
func scanKeyEpoch(rows *sql.Rows, tenantID storage.TenantID) (audit.ChainKeyEpoch, error) {
	return scanKeyEpochScanner(rows, tenantID)
}

// scanKeyEpochRow 는 *sql.Row 를 ChainKeyEpoch 로 변환합니다.
func scanKeyEpochRow(row *sql.Row, tenantID storage.TenantID) (audit.ChainKeyEpoch, error) {
	return scanKeyEpochScanner(row, tenantID)
}

type keyEpochScanner interface {
	Scan(dest ...any) error
}

func scanKeyEpochScanner(s keyEpochScanner, tenantID storage.TenantID) (audit.ChainKeyEpoch, error) {
	var (
		epoch          int64
		rowTenant      string
		keyID          string
		publicKeyHex   string
		keystoreHandle string
		createdStr     string
		revokedStr     sql.NullString
		createdBy      string
		auditEntrySeq  int64
	)
	if err := s.Scan(&epoch, &rowTenant, &keyID, &publicKeyHex, &keystoreHandle,
		&createdStr, &revokedStr, &createdBy, &auditEntrySeq); err != nil {
		return audit.ChainKeyEpoch{}, err
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdStr)
	if err != nil {
		return audit.ChainKeyEpoch{}, fmt.Errorf("audit: parse chain key created_at epoch=%d: %w", epoch, err)
	}

	out := audit.ChainKeyEpoch{
		Epoch:          epoch,
		TenantID:       storage.TenantID(rowTenant),
		KeyID:          keyID,
		PublicKeyHex:   publicKeyHex,
		KeystoreHandle: keystoreHandle,
		CreatedAt:      createdAt,
		CreatedBy:      createdBy,
		AuditEntrySeq:  auditEntrySeq,
	}
	if revokedStr.Valid {
		revokedAt, err := time.Parse(time.RFC3339Nano, revokedStr.String)
		if err != nil {
			return audit.ChainKeyEpoch{}, fmt.Errorf("audit: parse chain key revoked_at epoch=%d: %w", epoch, err)
		}
		out.RevokedAt = &revokedAt
	}
	// tenantID 인자 무시 — DB 의 tenant_id 가 진실원. caller 가 WHERE tenant_id = ? 를 강제했으므로 일관.
	_ = tenantID
	return out, nil
}
