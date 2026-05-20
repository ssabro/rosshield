-- E-MR (Phase 8) — Multi-region HA Stage 1 — cross-region replication metadata.
--
-- design: docs/design/notes/multi-region-ha-design.md (옵션 A 권장 = PG logical replication
-- + Route53 DNS, D-MR-1 ~ D-MR-5 default).
--
-- 본 round (Stage 1): 메타데이터 테이블만 도입. PG publication·subscription 자동 설정·
-- DNS hook 실 SDK 호출은 별 layer (Stage 3·4 carryover).
--
-- 의미:
--   - replication_replicas : region 별 replica role(primary/standby) + endpoint + 마지막
--     replay LSN/timestamp + heartbeat 추적.
--   - replication_failovers: failover 이력 (audit chain link 위함). initiated_by_user는
--     수동 failover trigger한 admin id. audit_entry_id는 audit.replication.failover entry
--     의 audit_entries.id 참조 (FK 강제는 안 함 — append-only audit table은 ON DELETE
--     호환 차원에서 soft link).
--
-- 멀티테넌시: 본 테이블은 인프라 메타로 tenant 글로벌 (P4 예외 — region 토폴로지는
-- 전체 deployment 공유). audit_entry는 tenant scope 유지 (system tenant).

CREATE TABLE replication_replicas (
    id                  BIGSERIAL PRIMARY KEY,
    region              TEXT NOT NULL,          -- e.g., 'us-west-2', 'ap-northeast-2'
    role                TEXT NOT NULL,          -- 'primary' | 'standby'
    endpoint            TEXT NOT NULL,          -- internal DNS / API base URL
    last_replay_lsn     TEXT,                   -- PG LSN cursor (16진 'X/X' 형식)
    last_replay_at      TIMESTAMPTZ,
    last_heartbeat_at   TIMESTAMPTZ,
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (region),
    CHECK (role IN ('primary', 'standby'))
);

CREATE INDEX idx_replication_replicas_role ON replication_replicas (role);

CREATE TABLE replication_failovers (
    id                  BIGSERIAL PRIMARY KEY,
    from_region         TEXT NOT NULL,
    to_region           TEXT NOT NULL,
    initiated_by_user   TEXT,                   -- 수동 failover trigger한 admin user id (UUID 텍스트)
    initiated_at        TIMESTAMPTZ NOT NULL,
    completed_at        TIMESTAMPTZ,
    reason              TEXT,
    audit_entry_id      BIGINT                  -- audit.replication.failover audit_entries.id soft link
);

CREATE INDEX idx_replication_failovers_initiated ON replication_failovers (initiated_at DESC);
