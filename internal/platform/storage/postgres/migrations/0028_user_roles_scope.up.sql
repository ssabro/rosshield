-- 0028_user_roles_scope.up.sql — user_roles 테이블에 fleet scope 컬럼 추가 (PG).
--
-- SQLite 0028 미러. design doc: docs/design/notes/rbac-fine-grained-design.md
-- §6.1 + §7 Stage 2. D-RBAC-2 권장 default = 2-level scope, D-RBAC-5 권장 default = 자동 변환.
--
-- 기존 row는 DEFAULT 'tenant' / '' 로 자동 분류 — 호환 보존.

ALTER TABLE user_roles ADD COLUMN scope_type TEXT NOT NULL DEFAULT 'tenant';
ALTER TABLE user_roles ADD COLUMN scope_id   TEXT NOT NULL DEFAULT '';

CREATE INDEX user_roles_scope ON user_roles(user_id, scope_type, scope_id);
