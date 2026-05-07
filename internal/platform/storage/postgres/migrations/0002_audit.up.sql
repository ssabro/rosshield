-- E22-B — SQLite 0002_audit.sql → PostgreSQL 변환.
-- 참조: docs/design/10-audit-and-observability.md §10.4·§10.5·§10.8
--       docs/design/04-domain-and-data-model.md §4.3 audit_entries
--
-- 변환 메모:
--   * BLOB                 → BYTEA (sha256 32B / Ed25519 64B)
--   * TEXT (RFC3339Nano)   → TIMESTAMPTZ
--   * SQLite RAISE(ABORT)  → PL/pgSQL RAISE EXCEPTION (P9 immutability)

-- 감사 엔트리: 테넌트당 단조 증가 seq, 해시 체인 연결.
CREATE TABLE audit_entries (
    tenant_id      TEXT        NOT NULL,
    seq            BIGINT      NOT NULL,
    occurred_at    TIMESTAMPTZ NOT NULL,
    actor_type     TEXT        NOT NULL, -- 'user' | 'api' | 'system' | 'anonymous'
    actor_id       TEXT        NOT NULL, -- us_... | ak_... | 'system' | '0.0.0.0'
    actor_ip       TEXT,
    actor_ua       TEXT,
    action         TEXT        NOT NULL, -- 'robot.create' | 'scan.execute' | ...
    target_type    TEXT        NOT NULL, -- 'robot' | 'scan' | 'tenant' | ...
    target_id      TEXT        NOT NULL,
    payload_digest BYTEA       NOT NULL, -- sha256 32B
    outcome        TEXT        NOT NULL, -- 'success' | 'failure' | 'partial'
    error_code     TEXT,
    error_message  TEXT,
    prev_hash      BYTEA       NOT NULL, -- sha256 32B (genesis = 32B 0x00)
    hash           BYTEA       NOT NULL, -- sha256 32B
    PRIMARY KEY (tenant_id, seq)
);

CREATE INDEX audit_entries_tenant_occurred
    ON audit_entries(tenant_id, occurred_at);

-- 체인 헤드: 테넌트당 1행. seq 할당과 head 추적의 단일 진실원.
CREATE TABLE audit_chain_heads (
    tenant_id  TEXT        PRIMARY KEY,
    seq        BIGINT      NOT NULL,
    hash       BYTEA       NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- Checkpoint 서명: 매시간 또는 중요 이벤트마다 head를 Ed25519로 서명.
CREATE TABLE audit_checkpoints (
    tenant_id     TEXT        NOT NULL,
    seq           BIGINT      NOT NULL,
    hash          BYTEA       NOT NULL, -- 서명 시점 head hash
    signed_at     TIMESTAMPTZ NOT NULL,
    signer_key_id TEXT        NOT NULL,
    signature     BYTEA       NOT NULL, -- Ed25519 서명 (64B)
    PRIMARY KEY (tenant_id, seq)
);

-- P9 불변성 강제: UPDATE/DELETE 차단 (PL/pgSQL 트리거).
CREATE OR REPLACE FUNCTION audit_entries_block_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit log is immutable';
END;
$$;

CREATE OR REPLACE FUNCTION audit_entries_block_delete()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit log is immutable';
END;
$$;

CREATE OR REPLACE FUNCTION audit_checkpoints_block_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit checkpoint is immutable';
END;
$$;

CREATE OR REPLACE FUNCTION audit_checkpoints_block_delete()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit checkpoint is immutable';
END;
$$;

CREATE TRIGGER audit_entries_no_update
    BEFORE UPDATE ON audit_entries
    FOR EACH ROW EXECUTE FUNCTION audit_entries_block_update();

CREATE TRIGGER audit_entries_no_delete
    BEFORE DELETE ON audit_entries
    FOR EACH ROW EXECUTE FUNCTION audit_entries_block_delete();

CREATE TRIGGER audit_checkpoints_no_update
    BEFORE UPDATE ON audit_checkpoints
    FOR EACH ROW EXECUTE FUNCTION audit_checkpoints_block_update();

CREATE TRIGGER audit_checkpoints_no_delete
    BEFORE DELETE ON audit_checkpoints
    FOR EACH ROW EXECUTE FUNCTION audit_checkpoints_block_delete();
