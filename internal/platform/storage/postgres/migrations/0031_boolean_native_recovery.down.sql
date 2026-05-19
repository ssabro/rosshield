-- E22-F R30-1.2 down — BOOLEAN → SMALLINT 회수 (rollback).
--
-- 운영 down은 거의 없음 (dev/CI rollback 시나리오만 가정). 비용 작아 작성.

ALTER TABLE roles ALTER COLUMN is_system DROP DEFAULT;
ALTER TABLE roles ALTER COLUMN is_system TYPE SMALLINT USING (CASE WHEN is_system THEN 1 ELSE 0 END);
ALTER TABLE roles ALTER COLUMN is_system SET DEFAULT 0;

ALTER TABLE compliance_profiles ALTER COLUMN enabled DROP DEFAULT;
ALTER TABLE compliance_profiles ALTER COLUMN enabled TYPE SMALLINT USING (CASE WHEN enabled THEN 1 ELSE 0 END);
ALTER TABLE compliance_profiles ALTER COLUMN enabled SET DEFAULT 1;

ALTER TABLE webhook_endpoints ALTER COLUMN enabled DROP DEFAULT;
ALTER TABLE webhook_endpoints ALTER COLUMN enabled TYPE SMALLINT USING (CASE WHEN enabled THEN 1 ELSE 0 END);
ALTER TABLE webhook_endpoints ALTER COLUMN enabled SET DEFAULT 1;

ALTER TABLE webhook_deliveries ALTER COLUMN succeeded DROP DEFAULT;
ALTER TABLE webhook_deliveries ALTER COLUMN succeeded TYPE SMALLINT USING (CASE WHEN succeeded THEN 1 ELSE 0 END);
ALTER TABLE webhook_deliveries ALTER COLUMN succeeded SET DEFAULT 0;

ALTER TABLE sso_providers ALTER COLUMN enabled DROP DEFAULT;
ALTER TABLE sso_providers ALTER COLUMN enabled TYPE SMALLINT USING (CASE WHEN enabled THEN 1 ELSE 0 END);
ALTER TABLE sso_providers ALTER COLUMN enabled SET DEFAULT 1;
