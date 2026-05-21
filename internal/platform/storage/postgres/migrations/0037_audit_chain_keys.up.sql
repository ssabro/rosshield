-- Phase 10.D-2 — audit chain key rotation 메타데이터 (epoch별 public key 보존).
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
-- 불변성: append-only + revoked_at 만 UPDATE 허용. UPDATE/DELETE 차단 트리거 (P9 + §8.1).
--
-- 초기 epoch=1 row: 마이그레이션 적용 시 placeholder bootstrap row 자동 insert.
-- 실 활성 signer 정보는 부트스트랩 코드가 후속 갱신 (revoked_at 만 update 허용 트리거 일관).

CREATE TABLE audit_chain_keys (
    epoch            BIGSERIAL    PRIMARY KEY,
    tenant_id        TEXT         NOT NULL,
    key_id           TEXT         NOT NULL,
    public_key_hex   TEXT         NOT NULL,
    keystore_handle  TEXT         NOT NULL,
    created_at       TEXT         NOT NULL,   -- RFC3339Nano UTC
    revoked_at       TEXT,                    -- nullable RFC3339Nano UTC
    created_by       TEXT         NOT NULL,   -- 'scheduler' | 'admin' | 'cli' | 'migration:0037'
    audit_entry_seq  BIGINT       NOT NULL,
    UNIQUE (tenant_id, epoch),
    UNIQUE (tenant_id, key_id)
);

CREATE INDEX idx_audit_chain_keys_tenant_created
    ON audit_chain_keys (tenant_id, created_at);

-- P9 불변성: revoked_at 외 column 변경 차단.
CREATE OR REPLACE FUNCTION audit_chain_keys_block_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.epoch           IS DISTINCT FROM OLD.epoch
       OR NEW.tenant_id        IS DISTINCT FROM OLD.tenant_id
       OR NEW.key_id           IS DISTINCT FROM OLD.key_id
       OR NEW.public_key_hex   IS DISTINCT FROM OLD.public_key_hex
       OR NEW.keystore_handle  IS DISTINCT FROM OLD.keystore_handle
       OR NEW.created_at       IS DISTINCT FROM OLD.created_at
       OR NEW.created_by       IS DISTINCT FROM OLD.created_by
       OR NEW.audit_entry_seq  IS DISTINCT FROM OLD.audit_entry_seq THEN
        RAISE EXCEPTION 'audit chain key is immutable (only revoked_at may change)';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION audit_chain_keys_block_delete()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit chain key is immutable';
END;
$$;

CREATE TRIGGER audit_chain_keys_no_update
    BEFORE UPDATE ON audit_chain_keys
    FOR EACH ROW EXECUTE FUNCTION audit_chain_keys_block_update();

CREATE TRIGGER audit_chain_keys_no_delete
    BEFORE DELETE ON audit_chain_keys
    FOR EACH ROW EXECUTE FUNCTION audit_chain_keys_block_delete();

-- 초기 epoch=1 bootstrap row. 실 활성 signer 정보는 부트스트랩 코드가 후속 갱신.
INSERT INTO audit_chain_keys
    (tenant_id, key_id, public_key_hex, keystore_handle, created_at, created_by, audit_entry_seq)
VALUES
    ('system', '__bootstrap__', '__bootstrap__', '__bootstrap__',
     to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
     'migration:0037', 0);
