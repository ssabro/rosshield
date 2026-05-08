-- E22-B — SQLite 0009_credentials.sql → PostgreSQL 변환.
-- 참조: docs/design/06-security-and-tenancy.md §6.5·§6.6
--       docs/design/notes/e5-robot-fleet-deepdive.md §3
--
-- 변환 메모:
--   * BLOB → BYTEA (encrypted_payload — AES-256-GCM ciphertext)
--   * TEXT (JSON encryption_meta) → TEXT
--   * TEXT (RFC3339Nano) → TEXT

CREATE TABLE credentials (
    id                  TEXT        NOT NULL,
    tenant_id           TEXT        NOT NULL,
    type                TEXT        NOT NULL,  -- 'password' | 'privateKey'
    encrypted_payload   BYTEA       NOT NULL,
    encryption_meta     TEXT       NOT NULL,  -- EncryptionMeta JSON (Version·Algorithm·KEKKeyID·AAD·DEKNonce·PayloadNonce·WrappedDEK·CreatedAt)
    rotation_due_at     TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    revoked_at          TEXT,           -- soft delete (R3-5)
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX credentials_tenant ON credentials(tenant_id);
