-- E22-B — 0010 down. 역순 DROP.
DROP INDEX IF EXISTS robots_tenant_host_port_active;
DROP INDEX IF EXISTS robots_tenant_fleet_name_active;
DROP INDEX IF EXISTS robots_credential;
DROP INDEX IF EXISTS robots_tenant_fleet;
DROP INDEX IF EXISTS robots_tenant;
DROP TABLE IF EXISTS robots;
