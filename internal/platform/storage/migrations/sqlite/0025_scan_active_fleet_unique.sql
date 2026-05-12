-- +goose Up
-- 0025_scan_active_fleet_unique.sql — fleet-level 동시 스캔 limit (race-proof).
--
-- 도메인 layer assertNoActiveFleetSession은 SELECT-then-INSERT 패턴이라 SQLite는
-- Tx 직렬 보장이 있지만 PostgreSQL은 동시 두 Tx가 동시에 SELECT 통과 후 둘 다 INSERT
-- 하는 race가 가능. 본 partial unique index는 DB 차원에서 그 race를 차단합니다.
--
-- 대상: 같은 (tenant_id, fleet_id) 조합에서 status가 pending/running인 row는 1개만 존재.
-- terminal(completed/failed/cancelled)은 자원 점유 X이므로 인덱스에서 제외.
--
-- SQLite는 3.8.0+ 부터 partial index 지원 (modernc.org/sqlite 최신 사용 → OK).

CREATE UNIQUE INDEX IF NOT EXISTS uq_scan_sessions_active_fleet
  ON scan_sessions (tenant_id, fleet_id)
  WHERE status IN ('pending', 'running');

-- +goose Down
DROP INDEX IF EXISTS uq_scan_sessions_active_fleet;
