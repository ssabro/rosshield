-- +goose Up
-- 0026_scan_severity_aggregate.sql — scan_sessions에 severity별 failed 카운트 4 컬럼 추가.
--
-- 배경: scans list 응답이 progress(total/completed/failed)만 노출 → 운영자가
-- list 화면에서 severity 분포(critical/high/medium/low)를 볼 수 없어 매번 detail 진입.
-- 본 컬럼은 scanrun terminal transition(completed/failed/cancelled) 시 atomic하게
-- scan_results JOIN pack_checks GROUP BY severity로 집계되어 list polling 비용 0.
--
-- 4-tier(critical/high/medium/low) — info는 CIS pack에 0건이고 Findings 페이지의
-- 5-tier info는 별 도메인(insight)이라 미포함 (D26-1 권장).
--
-- 참조: docs/design/notes/scans-severity-aggregate-design.md
--       docs/design/04-domain-and-data-model.md §4.2 ScanSession
--       docs/design/07-scan-engine-and-benchmarks.md §7.2~7.3
-- 결정 D26-1·D26-2·D26-3·D26-4 (사용자 합의 2026-05-14).

ALTER TABLE scan_sessions ADD COLUMN severity_critical_failed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_sessions ADD COLUMN severity_high_failed     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_sessions ADD COLUMN severity_medium_failed   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scan_sessions ADD COLUMN severity_low_failed      INTEGER NOT NULL DEFAULT 0;

-- 기존 sessions backfill — D26-2 권장(customer 0 시점, 마이그레이션 inline).
-- terminal sessions의 scan_results를 pack_checks severity별 그룹으로 집계.
-- 각 severity 컬럼별 별 UPDATE — SQLite UPDATE FROM 비호환 회피, 단일 path로 PG와 동일 동작.
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

-- +goose Down
-- SQLite 3.35+ 부터 ALTER TABLE DROP COLUMN 지원 (modernc.org/sqlite 최신 → OK).
ALTER TABLE scan_sessions DROP COLUMN severity_low_failed;
ALTER TABLE scan_sessions DROP COLUMN severity_medium_failed;
ALTER TABLE scan_sessions DROP COLUMN severity_high_failed;
ALTER TABLE scan_sessions DROP COLUMN severity_critical_failed;
