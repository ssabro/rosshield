-- E20-A Phase 3 — SSO (OIDC + SAML) PG 버전.
-- SQLite 0020_sso 변환: INTEGER enabled → BOOLEAN, INTEGER bool 그대로 SMALLINT.

CREATE TABLE sso_providers (
    id           TEXT NOT NULL,
    tenant_id    TEXT NOT NULL,
    type         TEXT NOT NULL
                     CHECK (type IN ('oidc','saml')),
    name         TEXT NOT NULL,
    enabled      SMALLINT NOT NULL DEFAULT 1,
    config_json  TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    UNIQUE (tenant_id, name)
);

CREATE INDEX sso_providers_tenant ON sso_providers(tenant_id);

CREATE TABLE sso_login_attempts (
    id              TEXT NOT NULL,
    tenant_id       TEXT NOT NULL,
    provider_id     TEXT NOT NULL,
    state           TEXT NOT NULL,
    pkce_verifier   TEXT,
    nonce           TEXT,
    relay_state     TEXT,
    created_at      TEXT NOT NULL,
    expires_at      TEXT NOT NULL,
    completed_at    TEXT,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (provider_id) REFERENCES sso_providers(id)
);

CREATE INDEX sso_login_attempts_state ON sso_login_attempts(state);
CREATE INDEX sso_login_attempts_tenant ON sso_login_attempts(tenant_id);

CREATE TABLE sso_external_identities (
    provider_id      TEXT NOT NULL,
    external_subject TEXT NOT NULL,
    tenant_id        TEXT NOT NULL,
    user_id          TEXT NOT NULL,
    email            TEXT NOT NULL DEFAULT '',
    first_seen_at    TEXT NOT NULL,
    last_seen_at     TEXT NOT NULL,
    PRIMARY KEY (provider_id, external_subject),
    FOREIGN KEY (provider_id) REFERENCES sso_providers(id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX sso_external_identities_tenant ON sso_external_identities(tenant_id);
CREATE INDEX sso_external_identities_user ON sso_external_identities(user_id);
