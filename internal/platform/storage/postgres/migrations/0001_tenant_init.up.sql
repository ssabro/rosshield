-- E22-A — PostgreSQL 첫 마이그레이션 (scaffold).
-- 목적: SQLite 0001_platform_init + 0003_tenant_user 를 PG 호환으로 변환한 첫 단계.
-- 후속 stage 에서 0002 audit, 0004 roles, … 를 차례로 변환합니다.
--
-- 변환 결정(SQLite → PG):
--   * INTEGER PRIMARY KEY AUTOINCREMENT → BIGSERIAL PRIMARY KEY (사용 안함; ULID/TEXT PK 유지)
--   * TEXT (RFC3339Nano UTC)            → TIMESTAMPTZ
--   * TEXT (JSON 본문)                   → JSONB
--   * BLOB                              → BYTEA (본 마이그레이션에서는 미사용)
--   * UNIQUE (a, b)                     → UNIQUE (a, b) 동일
--   * FOREIGN KEY ... REFERENCES        → 동일
--
-- 참조:
--   docs/design/04-domain-and-data-model.md §4.2·§4.3
--   docs/design/05-api-and-auth.md §5.7

-- 플랫폼 부트스트랩 KV (SQLite 0001_platform_init 와 동일 의미).
CREATE TABLE platform_info (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- 테넌트 (E3 Stage A).
CREATE TABLE tenants (
    id          TEXT        PRIMARY KEY,
    name        TEXT        NOT NULL,
    plan        TEXT        NOT NULL DEFAULT 'desktop_free',
    -- plan: 'desktop_free' | 'desktop_pro' | 'enterprise' | 'appliance'
    created_at  TIMESTAMPTZ NOT NULL,
    settings    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    features    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    retention   JSONB       NOT NULL DEFAULT '{}'::jsonb
);

-- 사용자.
CREATE TABLE users (
    id               TEXT        PRIMARY KEY,
    tenant_id        TEXT        NOT NULL,
    email            TEXT        NOT NULL,
    display_name     TEXT,
    auth_provider    TEXT        NOT NULL DEFAULT 'local',
    -- auth_provider: 'local' | 'oidc' | 'saml' | 'os'
    external_subject TEXT,
    password_hash    TEXT, -- argon2id encoded ($argon2id$...). local 만 채움.
    status           TEXT        NOT NULL DEFAULT 'active',
    -- status: 'active' | 'disabled' | 'invited'
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL,
    UNIQUE (tenant_id, email),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX users_tenant ON users(tenant_id);
