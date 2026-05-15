-- +goose Up
-- 0028_user_roles_scope.sql — user_roles 테이블에 fleet 단위 scope 컬럼 추가.
--
-- 배경: 세분 RBAC Stage 2 — design doc `rbac-fine-grained-design.md` §6.1 + §7 Stage 2.
-- 기존 RBAC는 user × role 단위 binding만 표현 가능 — fleet 단위 scope를 추가하여
-- "operator@fleet_a"·"fleet-admin@fleet_b" 같은 fleet 한정 권한을 표현합니다.
--
-- 정책 (D-RBAC-2 권장 default = 2-level scope, D-RBAC-5 권장 default = 자동 변환):
--   - scope_type='tenant' (기본값) — 모든 fleet에 implicit 적용 (tenant 글로벌 role).
--   - scope_type='fleet' — scope_id 가 fleet ID 한정.
--   - 기존 row(0027 이전 INSERT)는 DEFAULT 'tenant'로 자동 분류 — 호환 보존.
--
-- 인덱스: (user_id, scope_type, scope_id) 로 lookup O(log n) 보장.
-- INDEX는 partial이 아닌 일반 — scope_type='tenant'인 row에서 scope_id 빈 문자열도 정렬 cover.
--
-- scope_id를 NULL 대신 빈 문자열 ''로 default — SQLite의 COMPOSITE INDEX 동작 일관성 +
-- LookupByScope 쿼리에서 NULL 비교 회피.
--
-- 참조: docs/design/notes/rbac-fine-grained-design.md §6.1 + §7 Stage 2.

ALTER TABLE user_roles ADD COLUMN scope_type TEXT NOT NULL DEFAULT 'tenant';
ALTER TABLE user_roles ADD COLUMN scope_id   TEXT NOT NULL DEFAULT '';

CREATE INDEX user_roles_scope ON user_roles(user_id, scope_type, scope_id);

-- +goose Down
DROP INDEX IF EXISTS user_roles_scope;
-- SQLite는 ALTER TABLE DROP COLUMN을 3.35+에서 지원하지만 호환을 위해 보수적으로
-- 컬럼 그대로 둡니다. 본 마이그레이션은 forward-only 권장 (design doc §9.1).
