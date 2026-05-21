-- +goose Up
-- Phase 10.D-2 — audit chain key rotation 메타데이터 (epoch별 public key 보존) — SQLite.
--
-- design: docs/design/notes/audit-chain-rotation-automation-design.md §8.1 + §12 (옵션 C 채택).
--
-- 본 round (Stage 10.D-2): 메타데이터 테이블만 도입. signer hot-swap·scheduler는 별 stage.
--
-- 의미:
--   - tenant 별 epoch 단조 증가 (1부터 시작). 현 단일 system tenant 전제이나 멀티테넌시
--     일관 위해 tenant_id 컬럼 보존.
--   - key_id 는 Ed25519 KeyID ("key_" + hex(sha256(pub)[:8])).
--   - public_key_hex 는 Ed25519 public key 32B 의 hex encoding.
--   - keystore_handle 은 file path 또는 TPM handle.
--   - audit_entry_seq 는 rotation event 의 audit entry seq (epoch=1 bootstrap 은 0).
--
-- 불변성: append-only + revoked_at 만 UPDATE 허용. SQLite 는 WHEN 절에서 OLD/NEW 비교로 enforce.

CREATE TABLE audit_chain_keys (
    epoch            INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id        TEXT    NOT NULL,
    key_id           TEXT    NOT NULL,
    public_key_hex   TEXT    NOT NULL,
    keystore_handle  TEXT    NOT NULL,
    created_at       TEXT    NOT NULL,
    revoked_at       TEXT,
    created_by       TEXT    NOT NULL,
    audit_entry_seq  INTEGER NOT NULL,
    UNIQUE (tenant_id, epoch),
    UNIQUE (tenant_id, key_id)
);

CREATE INDEX idx_audit_chain_keys_tenant_created ON audit_chain_keys (tenant_id, created_at);

-- +goose StatementBegin
CREATE TRIGGER audit_chain_keys_no_update
    BEFORE UPDATE ON audit_chain_keys
    WHEN NEW.epoch IS NOT OLD.epoch
      OR NEW.tenant_id IS NOT OLD.tenant_id
      OR NEW.key_id IS NOT OLD.key_id
      OR NEW.public_key_hex IS NOT OLD.public_key_hex
      OR NEW.keystore_handle IS NOT OLD.keystore_handle
      OR NEW.created_at IS NOT OLD.created_at
      OR NEW.created_by IS NOT OLD.created_by
      OR NEW.audit_entry_seq IS NOT OLD.audit_entry_seq
    BEGIN
        SELECT RAISE(ABORT, 'audit chain key is immutable (only revoked_at may change)');
    END;
-- +goose StatementEnd

CREATE TRIGGER audit_chain_keys_no_delete BEFORE DELETE ON audit_chain_keys BEGIN SELECT RAISE(ABORT, 'audit chain key is immutable'); END;

INSERT INTO audit_chain_keys (tenant_id, key_id, public_key_hex, keystore_handle, created_at, created_by, audit_entry_seq) VALUES ('system', '__bootstrap__', '__bootstrap__', '__bootstrap__', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), 'migration:0037', 0);

-- +goose Down
DROP TRIGGER IF EXISTS audit_chain_keys_no_delete;
DROP TRIGGER IF EXISTS audit_chain_keys_no_update;
DROP INDEX IF EXISTS idx_audit_chain_keys_tenant_created;
DROP TABLE IF EXISTS audit_chain_keys;
