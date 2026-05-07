-- E22-B — SQLite 0006_auth_refresh.sql → PostgreSQL 변환.
-- 참조: docs/design/05-api-and-auth.md §5.7 (refresh allowlist/denylist)
--
-- 변환 메모:
--   * TEXT (RFC3339Nano) → TIMESTAMPTZ

CREATE TABLE auth_refresh_tokens (
    jti          TEXT        PRIMARY KEY,             -- "rt_<ULID>", JWT의 jti claim과 일치
    user_id      TEXT        NOT NULL,
    tenant_id    TEXT        NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,                          -- 설정되면 사용 불가
    created_at   TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (user_id)   REFERENCES users(id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX auth_refresh_user_tenant ON auth_refresh_tokens(tenant_id, user_id);
