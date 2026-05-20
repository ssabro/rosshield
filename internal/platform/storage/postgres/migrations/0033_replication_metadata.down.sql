-- E-MR Stage 1 down — replication metadata 폐기.

DROP INDEX IF EXISTS idx_replication_failovers_initiated;
DROP TABLE IF EXISTS replication_failovers;

DROP INDEX IF EXISTS idx_replication_replicas_role;
DROP TABLE IF EXISTS replication_replicas;
