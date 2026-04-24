-- +goose Up
-- E3 Stage D — Refresh token DB 매핑.
-- 참조: docs/design/05-api-and-auth.md §5.7 (refresh allowlist/denylist)
--       리서치 권장 패턴: rotation + reuse detection (탈취 신호 시 일괄 revoke)

CREATE TABLE auth_refresh_tokens (
    jti          TEXT    PRIMARY KEY,             -- "rt_<ULID>", JWT의 jti claim과 일치
    user_id      TEXT    NOT NULL,
    tenant_id    TEXT    NOT NULL,
    expires_at   TEXT    NOT NULL,                -- RFC3339Nano UTC
    revoked_at   TEXT,                            -- 설정되면 사용 불가 (rotation 또는 logout)
    created_at   TEXT    NOT NULL,
    FOREIGN KEY (user_id)   REFERENCES users(id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX auth_refresh_user_tenant ON auth_refresh_tokens(tenant_id, user_id);

-- +goose Down
DROP INDEX IF EXISTS auth_refresh_user_tenant;
DROP TABLE IF EXISTS auth_refresh_tokens;
