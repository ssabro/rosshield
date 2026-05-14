-- 0026_scan_severity_aggregate.down.sql — severity 카운트 4 컬럼 제거 (PG).

ALTER TABLE scan_sessions DROP COLUMN severity_low_failed;
ALTER TABLE scan_sessions DROP COLUMN severity_medium_failed;
ALTER TABLE scan_sessions DROP COLUMN severity_high_failed;
ALTER TABLE scan_sessions DROP COLUMN severity_critical_failed;
