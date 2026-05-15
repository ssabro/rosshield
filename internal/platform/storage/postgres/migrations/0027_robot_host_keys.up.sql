-- 0027_robot_host_keys.up.sql — TOFU host key trust 테이블 (PG).
--
-- SQLite 0027 미러. design doc: docs/design/notes/scanrun-ssh-integration-design.md
-- §5.5 + §6 Stage 1. D-SCAN-2 권장 default = TOFU.

CREATE TABLE robot_host_keys (
    id                   TEXT NOT NULL,
    tenant_id            TEXT NOT NULL,
    robot_id             TEXT NOT NULL,
    fingerprint_sha256   TEXT NOT NULL,
    key_type             TEXT NOT NULL,
    key_blob             BYTEA NOT NULL,
    first_seen_at        TIMESTAMPTZ NOT NULL,
    last_verified_at     TIMESTAMPTZ NOT NULL,
    trust_state          TEXT NOT NULL DEFAULT 'trusted',
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (robot_id) REFERENCES robots(id)
);

CREATE INDEX robot_host_keys_tenant_robot ON robot_host_keys(tenant_id, robot_id);

CREATE UNIQUE INDEX robot_host_keys_unique
    ON robot_host_keys(tenant_id, robot_id, fingerprint_sha256);
