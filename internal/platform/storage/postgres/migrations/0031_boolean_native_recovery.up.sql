-- E22-F R30-1.2 — BOOLEAN 회수 2차 (E22-F design doc 옵션 A 적용).
--
-- 1차 핫 path 회수(0024, R30-1.1)에서 보류된 5 boolean 컬럼을 PG-native BOOLEAN 으로 복원.
-- SMALLINT 0/1 vs BOOLEAN false/true의 query plan·storage 차이는 사실상 0이지만,
-- schema 의미 명확성과 driver-native API 호환을 위해 회수.
--
-- 호환성 전제:
--   - sqliterepo는 본 commit에서 Go bool BIND/SCAN 패턴으로 일괄 전환.
--   - WHERE 절은 parameterized bool 인자(`WHERE succeeded = ?`) — SQLite·PG 양 driver 호환.
--   - modernc.org/sqlite는 bool BIND를 INTEGER 0/1로, INTEGER → bool SCAN을 양방향 자동 캐스트.
--
-- 비대상:
--   - audit_chain_heads.current — partial unique index 조건의 핵심으로 별 epic(R30-1.3 또는
--     driver-aware repo 진입 시 처리). 본 commit 대상 5 컬럼과 무관한 추가 위험 회피.
--
-- 설계: docs/design/notes/e22-f-boolean-recovery-design.md (옵션 A, Stage 1+2 통합).

-- 1. roles.is_system (DEFAULT 0/false)
ALTER TABLE roles ALTER COLUMN is_system DROP DEFAULT;
ALTER TABLE roles ALTER COLUMN is_system TYPE BOOLEAN USING (is_system <> 0);
ALTER TABLE roles ALTER COLUMN is_system SET DEFAULT FALSE;

-- 2. compliance_profiles.enabled (DEFAULT 1/true)
ALTER TABLE compliance_profiles ALTER COLUMN enabled DROP DEFAULT;
ALTER TABLE compliance_profiles ALTER COLUMN enabled TYPE BOOLEAN USING (enabled <> 0);
ALTER TABLE compliance_profiles ALTER COLUMN enabled SET DEFAULT TRUE;

-- 3. webhook_endpoints.enabled (DEFAULT 1/true)
ALTER TABLE webhook_endpoints ALTER COLUMN enabled DROP DEFAULT;
ALTER TABLE webhook_endpoints ALTER COLUMN enabled TYPE BOOLEAN USING (enabled <> 0);
ALTER TABLE webhook_endpoints ALTER COLUMN enabled SET DEFAULT TRUE;

-- 4. webhook_deliveries.succeeded (DEFAULT 0/false)
ALTER TABLE webhook_deliveries ALTER COLUMN succeeded DROP DEFAULT;
ALTER TABLE webhook_deliveries ALTER COLUMN succeeded TYPE BOOLEAN USING (succeeded <> 0);
ALTER TABLE webhook_deliveries ALTER COLUMN succeeded SET DEFAULT FALSE;

-- 5. sso_providers.enabled (DEFAULT 1/true)
ALTER TABLE sso_providers ALTER COLUMN enabled DROP DEFAULT;
ALTER TABLE sso_providers ALTER COLUMN enabled TYPE BOOLEAN USING (enabled <> 0);
ALTER TABLE sso_providers ALTER COLUMN enabled SET DEFAULT TRUE;
