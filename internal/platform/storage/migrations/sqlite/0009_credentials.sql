-- +goose Up
-- E5 Stage B — Credential 도메인 핵심 테이블 (KEK/DEK 2계층 암호화).
-- 참조: docs/design/06-security-and-tenancy.md §6.5·§6.6
--       docs/design/notes/e5-robot-fleet-deepdive.md §3 (R3-1·R3-2·R3-3)
--
-- encrypted_payload는 AES-256-GCM ciphertext (CredentialMaterial JSON 평문).
-- encryption_meta는 EncryptionMeta JSON (Version·Algorithm·KEKKeyID·AAD·DEKNonce·PayloadNonce·WrappedDEK·CreatedAt).
-- WrappedDEK는 KEK로 wrap된 random per-record DEK — Phase 1은 Tenant Key 생략(R3-2),
-- KEK→DEK 2계층. AAD에 tenantId·credentialId 포함해 cross-credential 키 재사용 차단.

CREATE TABLE credentials (
    id                  TEXT NOT NULL,
    tenant_id           TEXT NOT NULL,
    type                TEXT NOT NULL,  -- 'password' | 'privateKey'
    encrypted_payload   BLOB NOT NULL,
    encryption_meta     TEXT NOT NULL,  -- JSON
    rotation_due_at     TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    revoked_at          TEXT,           -- soft delete (R3-5)
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX credentials_tenant ON credentials(tenant_id);

-- +goose Down
DROP INDEX IF EXISTS credentials_tenant;
DROP TABLE IF EXISTS credentials;
