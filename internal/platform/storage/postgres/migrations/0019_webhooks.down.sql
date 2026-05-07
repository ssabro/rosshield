-- E22-B — 0019 down. 역순 DROP.
DROP INDEX IF EXISTS webhook_deliveries_tenant_event;
DROP INDEX IF EXISTS webhook_deliveries_endpoint_created;
DROP INDEX IF EXISTS webhook_deliveries_pending;
DROP TABLE IF EXISTS webhook_deliveries;
DROP INDEX IF EXISTS webhook_endpoints_tenant_created;
DROP TABLE IF EXISTS webhook_endpoints;
