-- E22-A — 0001 down. 역순 DROP. CASCADE 미사용(의존성 명시 유지).
DROP INDEX IF EXISTS users_tenant;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;
DROP TABLE IF EXISTS platform_info;
