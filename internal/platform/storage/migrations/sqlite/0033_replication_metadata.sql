-- +goose Up
-- E-MR (Phase 8) — Multi-region HA Stage 1 — cross-region replication metadata (SQLite).
--
-- 참조: docs/design/notes/multi-region-ha-design.md (옵션 A).
-- 본 round (Stage 1): 메타데이터 테이블만 도입. publication/DNS 자동화는 carryover.
--
-- SQLite는 단일 region single-instance 가정이지만 schema 호환을 위해 동등 테이블 보유 —
-- standby/primary role 식별 정도만 활용 가능 (replication 자체는 PG 전용).

CREATE TABLE replication_replicas (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    region              TEXT NOT NULL,
    role                TEXT NOT NULL,          -- 'primary' | 'standby'
    endpoint            TEXT NOT NULL,
    last_replay_lsn     TEXT,
    last_replay_at      TEXT,                   -- RFC3339Nano UTC
    last_heartbeat_at   TEXT,                   -- RFC3339Nano UTC
    enabled             INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT NOT NULL,
    UNIQUE (region),
    CHECK (role IN ('primary', 'standby'))
);

CREATE INDEX idx_replication_replicas_role ON replication_replicas (role);

CREATE TABLE replication_failovers (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    from_region         TEXT NOT NULL,
    to_region           TEXT NOT NULL,
    initiated_by_user   TEXT,
    initiated_at        TEXT NOT NULL,          -- RFC3339Nano UTC
    completed_at        TEXT,                   -- RFC3339Nano UTC
    reason              TEXT,
    audit_entry_id      INTEGER
);

CREATE INDEX idx_replication_failovers_initiated ON replication_failovers (initiated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_replication_failovers_initiated;
DROP TABLE IF EXISTS replication_failovers;

DROP INDEX IF EXISTS idx_replication_replicas_role;
DROP TABLE IF EXISTS replication_replicas;
