-- E22-B — 0011 down. 역순 DROP.
DROP INDEX IF EXISTS scan_results_session;
DROP INDEX IF EXISTS scan_results_session_robot_check;
DROP TABLE IF EXISTS scan_results;
DROP INDEX IF EXISTS scan_sessions_tenant_status_created;
DROP INDEX IF EXISTS scan_sessions_tenant_fleet_created;
DROP TABLE IF EXISTS scan_sessions;
