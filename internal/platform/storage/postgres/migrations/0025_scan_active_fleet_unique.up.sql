-- 0025_scan_active_fleet_unique.up.sql — fleet-level 동시 스캔 limit (race-proof, PG).
--
-- 같은 (tenant_id, fleet_id) 조합에서 status가 pending/running인 row는 1개만 허용.
-- 도메인 layer SELECT-then-INSERT 패턴의 PostgreSQL race 차단 — 두 동시 Tx가
-- SELECT 통과해도 두 번째 INSERT는 unique violation으로 거부됨.
--
-- terminal(completed/failed/cancelled)은 자원 점유 X이므로 인덱스에서 제외 (partial index).

CREATE UNIQUE INDEX IF NOT EXISTS uq_scan_sessions_active_fleet
  ON scan_sessions (tenant_id, fleet_id)
  WHERE status IN ('pending', 'running');
