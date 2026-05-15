-- 0028_user_roles_scope.down.sql — user_roles fleet scope 컬럼 롤백 (PG).

DROP INDEX IF EXISTS user_roles_scope;
ALTER TABLE user_roles DROP COLUMN IF EXISTS scope_id;
ALTER TABLE user_roles DROP COLUMN IF EXISTS scope_type;
