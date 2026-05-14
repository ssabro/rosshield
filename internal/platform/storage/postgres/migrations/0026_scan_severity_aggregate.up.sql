-- 0026_scan_severity_aggregate.up.sql — scan_sessions에 severity별 failed 카운트 4 컬럼 추가 (PG).
--
-- SQLite 0026 미러. ALTER TABLE ADD COLUMN + backfill 둘 다 PG 표준 호환.
-- design doc: docs/design/notes/scans-severity-aggregate-design.md
-- 결정 D26-1·D26-2·D26-3·D26-4 (사용자 합의 2026-05-14).

ALTER TABLE scan_sessions ADD COLUMN severity_critical_failed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_sessions ADD COLUMN severity_high_failed     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_sessions ADD COLUMN severity_medium_failed   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_sessions ADD COLUMN severity_low_failed      INTEGER NOT NULL DEFAULT 0;

-- 기존 sessions backfill — D26-2 권장. SQLite와 동일 패턴 유지(driver 차이 없는 ANSI SQL).
UPDATE scan_sessions
SET severity_critical_failed = COALESCE(
    (SELECT COUNT(*) FROM scan_results sr
     JOIN pack_checks pc ON pc.id = sr.pack_check_id
     WHERE sr.session_id = scan_sessions.id
       AND sr.outcome = 'fail'
       AND pc.severity = 'critical'), 0);

UPDATE scan_sessions
SET severity_high_failed = COALESCE(
    (SELECT COUNT(*) FROM scan_results sr
     JOIN pack_checks pc ON pc.id = sr.pack_check_id
     WHERE sr.session_id = scan_sessions.id
       AND sr.outcome = 'fail'
       AND pc.severity = 'high'), 0);

UPDATE scan_sessions
SET severity_medium_failed = COALESCE(
    (SELECT COUNT(*) FROM scan_results sr
     JOIN pack_checks pc ON pc.id = sr.pack_check_id
     WHERE sr.session_id = scan_sessions.id
       AND sr.outcome = 'fail'
       AND pc.severity = 'medium'), 0);

UPDATE scan_sessions
SET severity_low_failed = COALESCE(
    (SELECT COUNT(*) FROM scan_results sr
     JOIN pack_checks pc ON pc.id = sr.pack_check_id
     WHERE sr.session_id = scan_sessions.id
       AND sr.outcome = 'fail'
       AND pc.severity = 'low'), 0);
