-- +goose Up
-- E3 Stage A — tenant + user 도메인의 핵심 테이블.
-- 참조: docs/design/04-domain-and-data-model.md §4.2·§4.3
--       docs/design/05-api-and-auth.md §5.7

CREATE TABLE tenants (
    id          TEXT    PRIMARY KEY,
    name        TEXT    NOT NULL,
    plan        TEXT    NOT NULL DEFAULT 'desktop_free',
    -- plan: 'desktop_free' | 'desktop_pro' | 'enterprise' | 'appliance'
    created_at  TEXT    NOT NULL, -- RFC3339Nano UTC
    settings    TEXT    NOT NULL DEFAULT '{}', -- JSON
    features    TEXT    NOT NULL DEFAULT '{}', -- JSON
    retention   TEXT    NOT NULL DEFAULT '{}'  -- JSON
);

CREATE TABLE users (
    id               TEXT    PRIMARY KEY,
    tenant_id        TEXT    NOT NULL,
    email            TEXT    NOT NULL,
    display_name     TEXT,
    auth_provider    TEXT    NOT NULL DEFAULT 'local',
    -- auth_provider: 'local' | 'oidc' | 'saml' | 'os'
    external_subject TEXT,
    password_hash    TEXT, -- argon2id encoded ($argon2id$v=19$m=65536,t=3,p=1$<salt>$<hash>). local만 채움.
    status           TEXT    NOT NULL DEFAULT 'active',
    -- status: 'active' | 'disabled' | 'invited'
    created_at       TEXT    NOT NULL,
    updated_at       TEXT    NOT NULL,
    UNIQUE (tenant_id, email),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX users_tenant ON users(tenant_id);

-- +goose Down
DROP INDEX IF EXISTS users_tenant;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;
