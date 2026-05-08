-- E22-B — SQLite 0005_api_keys.sql → PostgreSQL 변환.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 ApiKey
--       docs/design/05-api-and-auth.md §5.9
--
-- 변환 메모:
--   * TEXT (JSON array) → TEXT (scopes)
--   * TEXT (RFC3339Nano) → TEXT (expires_at, last_used_at, created_at, revoked_at)

CREATE TABLE api_keys (
    id            TEXT        PRIMARY KEY,             -- ak_<ULID>
    tenant_id     TEXT        NOT NULL,
    name          TEXT        NOT NULL,                -- 사용자 가시 라벨
    prefix        TEXT        NOT NULL,                -- "fg_live_XXXX" 12자, 사용자 표시용
    hashed        TEXT        NOT NULL,                -- argon2id encoded
    scopes        TEXT       NOT NULL DEFAULT '[]',
    expires_at    TEXT,                          -- NULL = 무기한
    last_used_at  TEXT,                          -- 마지막 인증 성공 시각
    created_by    TEXT        NOT NULL,                 -- 발급한 user ID (us_...)
    created_at    TEXT NOT NULL,
    revoked_at    TEXT,                          -- soft delete
    UNIQUE (tenant_id, prefix),
    FOREIGN KEY (tenant_id)  REFERENCES tenants(id),
    FOREIGN KEY (created_by) REFERENCES users(id)
);

CREATE INDEX api_keys_tenant ON api_keys(tenant_id);
CREATE INDEX api_keys_prefix ON api_keys(prefix);
