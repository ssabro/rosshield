-- +goose Up
-- E20-A Phase 3 — SSO (OIDC + SAML) 도메인 골격.
-- 참조: docs/design/phase3-backlog.md E20
--       docs/design/05-api-and-auth.md §5.7 (auth_provider/external_subject)
--       docs/design/06-security-and-tenancy.md §6 (멀티테넌시)
--
-- 모델:
--   - sso_providers: tenant 단위 IdP 설정. type ∈ {oidc, saml}. config_json은 IdP 종속(OIDC: issuer/clientID/redirectURI; SAML: metadata XML/URL).
--   - sso_login_attempts: state·PKCE·nonce·RelayState 영속(짧은 TTL 5분). callback에서 검증.
--   - sso_external_identities: IdP의 sub(또는 NameID) → users.id 매핑. last_seen_at만 갱신 가능(append-once on insert).
--
-- 도메인 결합 (P5):
--   sso 서브패키지(internal/domain/tenant/sso)는 audit.Service 직접 import 금지.
--   bootstrap이 audit emitter adapter를 주입.
--
-- 멀티테넌시 (P4):
--   모든 row에 tenant_id 강제. cross-tenant 격리는 application layer에서 WHERE 강제 + index 활용.
--
-- 불변성 (P9):
--   login_attempts·external_identities는 append-only가 원칙. 단,
--     - login_attempts.completed_at: 한 번 NULL → timestamp 전이만 허용(스캐폴드 단계는 정책만 명시).
--     - external_identities.last_seen_at: upsert로 갱신 허용(원칙 9 예외 — 'last seen'은 사실 갱신).

CREATE TABLE sso_providers (
    id           TEXT NOT NULL,                -- "ssop_<ULID>"
    tenant_id    TEXT NOT NULL,
    type         TEXT NOT NULL
                     CHECK (type IN ('oidc','saml')),
    name         TEXT NOT NULL,                -- 사용자 라벨 ("Google Workspace", "Okta - Engineering")
    enabled      INTEGER NOT NULL DEFAULT 1,   -- 0/1 boolean
    config_json  TEXT NOT NULL DEFAULT '{}',   -- OIDC: {issuer,clientId,redirectUri,scopes[]}; SAML: {metadataUrl|metadataXml,acsUrl}
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    UNIQUE (tenant_id, name)                   -- tenant 안에서 라벨 unique
);

CREATE INDEX sso_providers_tenant ON sso_providers(tenant_id);

CREATE TABLE sso_login_attempts (
    id              TEXT NOT NULL,             -- "ssoa_<ULID>"
    tenant_id       TEXT NOT NULL,             -- denormalized — provider 통한 tenant join 회피
    provider_id     TEXT NOT NULL,
    state           TEXT NOT NULL,             -- CSRF/IdP-callback correlation key
    pkce_verifier   TEXT,                      -- OIDC PKCE — code_verifier(평문, TTL 짧음). NULL=SAML.
    nonce           TEXT,                      -- OIDC nonce(id_token nonce 검증). NULL=SAML.
    relay_state     TEXT,                      -- SAML RelayState(post-login redirect URL 등). NULL=OIDC.
    created_at      TEXT NOT NULL,
    expires_at      TEXT NOT NULL,             -- 통상 5분
    completed_at    TEXT,                      -- NULL=미사용. 채워지면 재사용 거부.
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (provider_id) REFERENCES sso_providers(id)
);

-- state는 callback에서 빠른 lookup. 글로벌 unique는 강제하지 않음(충돌은 IdP 측에서 거의 없음 + tenant 격리).
CREATE INDEX sso_login_attempts_state ON sso_login_attempts(state);
CREATE INDEX sso_login_attempts_tenant ON sso_login_attempts(tenant_id);

CREATE TABLE sso_external_identities (
    provider_id      TEXT NOT NULL,
    external_subject TEXT NOT NULL,            -- OIDC sub claim 또는 SAML NameID
    tenant_id        TEXT NOT NULL,            -- denormalized — cross-tenant 격리 강제
    user_id          TEXT NOT NULL,            -- users.id (E20-B/C에서 매핑/생성)
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

-- +goose Down
DROP INDEX IF EXISTS sso_external_identities_user;
DROP INDEX IF EXISTS sso_external_identities_tenant;
DROP TABLE IF EXISTS sso_external_identities;
DROP INDEX IF EXISTS sso_login_attempts_tenant;
DROP INDEX IF EXISTS sso_login_attempts_state;
DROP TABLE IF EXISTS sso_login_attempts;
DROP INDEX IF EXISTS sso_providers_tenant;
DROP TABLE IF EXISTS sso_providers;
