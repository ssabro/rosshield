-- +goose Up
-- 0029_sso_group_mappings.sql — SSO group → role 자동 매핑 + user_roles.source 추적.
--
-- 배경: RBAC fleet 정밀화 후속 Stage 4 — design doc
-- `docs/design/notes/rbac-fleet-scope-precision-design.md` §6.1 + §7 Stage 4 + D-RBACEX-5/7/8.
--
-- 두 가지 추가:
--
--  1. user_roles.source 컬럼 — binding 출처 추적 (D-RBACEX-7 권장 default = B).
--     · 'manual' (기본값) — 운영자가 admin UI / API로 명시 할당.
--     · 'sso'             — SSO group 매핑 흐름이 자동 생성.
--     기존 row(0028 이전 INSERT)는 모두 DEFAULT 'manual' — admin 수동 할당 의미 보존.
--     SSO group sync는 source='sso' row만 갱신(insert/delete)하여 manual binding은 보존.
--
--  2. sso_group_role_mappings 테이블 (D-RBACEX-5 권장 default = A 명시 mapping).
--     · IdP claim group 값 → (role_id, scope_type, scope_id) 매핑.
--     · UNIQUE(provider_id, group_value, role_id, scope_type, scope_id) — 같은 group이
--       다른 (role, scope) 조합에 매핑되는 multi-binding 허용 (예: "fleet-admin-warehouse-a"
--       group이 fleet-admin@flt_A + auditor@flt_A 둘 다 할당).
--     · 같은 매핑 5개 키 조합 중복 INSERT는 차단 — 멱등 운영.
--
-- DDD 경계 (P5):
--   sso_group_role_mappings 테이블 자체는 sso 도메인 sub-package(internal/domain/tenant/sso)
--   에서 사용하지만, 본 마이그레이션은 platform 마이그레이션 디렉토리에 둡니다 — 도메인
--   분리는 코드 경계, 마이그레이션은 storage 레이어 일관 관리(0028 패턴 일관).
--
-- 멀티테넌시 (P4):
--   tenant_id 컬럼 + FK → tenants(id). cross-tenant lookup은 application layer에서 차단.
--
-- 참조: docs/design/notes/rbac-fleet-scope-precision-design.md §6.1 + §7 Stage 4.

-- 1. user_roles.source 컬럼 추가 — 'manual' DEFAULT.
ALTER TABLE user_roles ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';

-- 2. sso_group_role_mappings — group → role binding 매핑 테이블.
CREATE TABLE sso_group_role_mappings (
    id           TEXT NOT NULL,                                 -- "sgm_<ULID>"
    tenant_id    TEXT NOT NULL,
    provider_id  TEXT NOT NULL,                                 -- sso_providers(id)
    group_value  TEXT NOT NULL,                                 -- IdP claim group ("fleet-admin-warehouse-a")
    role_id      TEXT NOT NULL,                                 -- roles(id)
    scope_type   TEXT NOT NULL DEFAULT 'tenant'
                     CHECK (scope_type IN ('tenant','fleet')),  -- ScopeType enum
    scope_id     TEXT NOT NULL DEFAULT '',                      -- scope_type='fleet'이면 fleet ID, 'tenant'이면 ''
    created_at   TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (provider_id) REFERENCES sso_providers(id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    UNIQUE (provider_id, group_value, role_id, scope_type, scope_id)
);

-- provider별 매핑 lookup (login 시 group claim → bindings 결정).
CREATE INDEX sso_group_role_mappings_provider ON sso_group_role_mappings(provider_id);
-- tenant 단위 admin UI 리스트 (CRUD 페이지).
CREATE INDEX sso_group_role_mappings_tenant ON sso_group_role_mappings(tenant_id);

-- +goose Down
DROP INDEX IF EXISTS sso_group_role_mappings_tenant;
DROP INDEX IF EXISTS sso_group_role_mappings_provider;
DROP TABLE IF EXISTS sso_group_role_mappings;
-- user_roles.source 컬럼은 forward-only — SQLite ALTER DROP COLUMN 회피 (호환).
