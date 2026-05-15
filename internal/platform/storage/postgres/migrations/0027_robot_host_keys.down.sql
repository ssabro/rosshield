-- 0027_robot_host_keys.down.sql — TOFU host key trust 테이블 롤백 (PG).

DROP INDEX IF EXISTS robot_host_keys_unique;
DROP INDEX IF EXISTS robot_host_keys_tenant_robot;
DROP TABLE IF EXISTS robot_host_keys;
