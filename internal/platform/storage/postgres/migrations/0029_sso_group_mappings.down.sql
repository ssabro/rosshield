-- 0029_sso_group_mappings.down.sql — SSO group 매핑 + user_roles.source 롤백 (PG).

DROP INDEX IF EXISTS sso_group_role_mappings_tenant;
DROP INDEX IF EXISTS sso_group_role_mappings_provider;
DROP TABLE IF EXISTS sso_group_role_mappings;
ALTER TABLE user_roles DROP COLUMN IF EXISTS source;
