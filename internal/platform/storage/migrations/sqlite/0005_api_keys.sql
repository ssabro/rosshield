-- +goose Up
-- E3 Stage C — ApiKey 발급·검증·revoke.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 ApiKey
--       docs/design/05-api-and-auth.md §5.9

CREATE TABLE api_keys (
    id            TEXT    PRIMARY KEY,         -- ak_<ULID>
    tenant_id     TEXT    NOT NULL,
    name          TEXT    NOT NULL,            -- 사용자 가시 라벨 (예: "ci-scanner")
    prefix        TEXT    NOT NULL,            -- "fg_live_XXXX" 12자, 사용자 표시용
    hashed        TEXT    NOT NULL,            -- argon2id encoded — 발급 raw token의 해시
    scopes        TEXT    NOT NULL DEFAULT '[]', -- JSON array of permission strings (빈 = 발급자 권한 그대로)
    expires_at    TEXT,                         -- RFC3339Nano UTC, NULL = 무기한
    last_used_at  TEXT,                         -- 마지막 인증 성공 시각 (Phase 1는 UPDATE 안 함, 후속)
    created_by    TEXT    NOT NULL,             -- 발급한 user ID (us_...)
    created_at    TEXT    NOT NULL,
    revoked_at    TEXT,                         -- 설정되면 인증 거부 (soft delete, P9: row 삭제 안 함)
    UNIQUE (tenant_id, prefix),
    FOREIGN KEY (tenant_id)  REFERENCES tenants(id),
    FOREIGN KEY (created_by) REFERENCES users(id)
);

CREATE INDEX api_keys_tenant ON api_keys(tenant_id);
CREATE INDEX api_keys_prefix ON api_keys(prefix);

-- +goose Down
DROP INDEX IF EXISTS api_keys_prefix;
DROP INDEX IF EXISTS api_keys_tenant;
DROP TABLE IF EXISTS api_keys;
