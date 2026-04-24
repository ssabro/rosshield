-- +goose Up
-- E2 Audit 도메인 — 해시 체인 append-only.
-- 참조: docs/design/10-audit-and-observability.md §10.4·§10.5·§10.8
--       docs/design/04-domain-and-data-model.md §4.3 audit_entries

-- 감사 엔트리: 테넌트당 단조 증가 seq, 해시 체인 연결.
-- payload_digest·prev_hash·hash는 모두 32바이트 sha256 raw bytes.
CREATE TABLE audit_entries (
    tenant_id      TEXT    NOT NULL,
    seq            INTEGER NOT NULL,
    occurred_at    TEXT    NOT NULL, -- RFC3339Nano UTC ('2026-04-24T...')
    actor_type     TEXT    NOT NULL, -- 'user' | 'api' | 'system' | 'anonymous'
    actor_id       TEXT    NOT NULL, -- us_... | ak_... | 'system' | '0.0.0.0'
    actor_ip       TEXT,
    actor_ua       TEXT,
    action         TEXT    NOT NULL, -- 'robot.create' | 'scan.execute' | ...
    target_type    TEXT    NOT NULL, -- 'robot' | 'scan' | 'tenant' | ...
    target_id      TEXT    NOT NULL,
    payload_digest BLOB    NOT NULL, -- sha256 32B
    outcome        TEXT    NOT NULL, -- 'success' | 'failure' | 'partial'
    error_code     TEXT,
    error_message  TEXT,
    prev_hash      BLOB    NOT NULL, -- sha256 32B (genesis = 32B 0x00)
    hash           BLOB    NOT NULL, -- sha256 32B
    PRIMARY KEY (tenant_id, seq)
);

CREATE INDEX audit_entries_tenant_occurred
    ON audit_entries(tenant_id, occurred_at);

-- 체인 헤드: 테넌트당 1행. seq 할당과 head 추적의 단일 진실원.
-- Append 시: SELECT head → INSERT entry(seq=head.seq+1) → UPSERT head.
CREATE TABLE audit_chain_heads (
    tenant_id  TEXT    PRIMARY KEY,
    seq        INTEGER NOT NULL,
    hash       BLOB    NOT NULL,
    updated_at TEXT    NOT NULL
);

-- Checkpoint 서명: 매시간 또는 중요 이벤트마다 head를 Ed25519로 서명.
CREATE TABLE audit_checkpoints (
    tenant_id     TEXT    NOT NULL,
    seq           INTEGER NOT NULL,
    hash          BLOB    NOT NULL, -- 서명 시점 head hash
    signed_at     TEXT    NOT NULL, -- RFC3339Nano UTC
    signer_key_id TEXT    NOT NULL,
    signature     BLOB    NOT NULL, -- Ed25519 서명 (64B)
    PRIMARY KEY (tenant_id, seq)
);

-- P9 불변성: UPDATE/DELETE 차단 (§10.8).
CREATE TRIGGER audit_entries_no_update
    BEFORE UPDATE ON audit_entries
    BEGIN SELECT RAISE(ABORT, 'audit log is immutable'); END;

CREATE TRIGGER audit_entries_no_delete
    BEFORE DELETE ON audit_entries
    BEGIN SELECT RAISE(ABORT, 'audit log is immutable'); END;

CREATE TRIGGER audit_checkpoints_no_update
    BEFORE UPDATE ON audit_checkpoints
    BEGIN SELECT RAISE(ABORT, 'audit checkpoint is immutable'); END;

CREATE TRIGGER audit_checkpoints_no_delete
    BEFORE DELETE ON audit_checkpoints
    BEGIN SELECT RAISE(ABORT, 'audit checkpoint is immutable'); END;

-- +goose Down
DROP TRIGGER IF EXISTS audit_checkpoints_no_delete;
DROP TRIGGER IF EXISTS audit_checkpoints_no_update;
DROP TRIGGER IF EXISTS audit_entries_no_delete;
DROP TRIGGER IF EXISTS audit_entries_no_update;
DROP TABLE IF EXISTS audit_checkpoints;
DROP TABLE IF EXISTS audit_chain_heads;
DROP TABLE IF EXISTS audit_entries;
