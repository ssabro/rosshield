-- +goose Up
-- E6 Stage C — Scan 도메인 핵심 테이블.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 ScanSession/ScanResult
--       docs/design/07-scan-engine-and-benchmarks.md §7.2·§7.3
--       docs/design/notes/e6-ssh-scan-deepdive.md §5·§9
--       phase1-backlog.md E6
-- 결정 R5-1~R5-7 (사용자 합의 2026-04-27).

-- scan_sessions: 스캔 실행 단위. FSM = pending → running → {completed | failed | cancelled}.
CREATE TABLE scan_sessions (
    id                  TEXT NOT NULL,                -- "scan_<ULID>" (R5-1)
    tenant_id           TEXT NOT NULL,
    fleet_id            TEXT NOT NULL,
    pack_id             TEXT NOT NULL,
    trigger             TEXT NOT NULL DEFAULT 'manual'
                            CHECK (trigger IN ('manual','schedule','event')), -- R5-7
    status              TEXT NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending','running','completed','failed','cancelled')),
    progress_total      INTEGER NOT NULL DEFAULT 0,   -- robot × check 총 작업 수
    progress_completed  INTEGER NOT NULL DEFAULT 0,
    progress_failed     INTEGER NOT NULL DEFAULT 0,
    failure_reason      TEXT NOT NULL DEFAULT '',     -- failed/cancelled 사유
    created_at          TEXT NOT NULL,                -- RFC3339Nano UTC
    updated_at          TEXT NOT NULL,
    started_at          TEXT,                         -- pending → running 전이 시점
    completed_at        TEXT,                         -- terminal 전이 시점
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (fleet_id) REFERENCES fleets(id),
    FOREIGN KEY (pack_id) REFERENCES packs(id)
);

-- ListSessions의 두 핵심 쿼리 패턴 (R5-6):
--   1) fleet 별 최근 → (tenant_id, fleet_id, created_at DESC)
--   2) status 별 최근 (running·pending 모니터링) → (tenant_id, status, created_at DESC)
-- started_at 대신 created_at을 정렬 키로 채택 — pending에서는 started_at이 NULL이라
-- SQLite의 NULLS-FIRST 동작과 충돌. created_at은 항상 채워지므로 안정적.
CREATE INDEX scan_sessions_tenant_fleet_created
    ON scan_sessions(tenant_id, fleet_id, created_at DESC);
CREATE INDEX scan_sessions_tenant_status_created
    ON scan_sessions(tenant_id, status, created_at DESC);

-- scan_results: 세션 내 (robot × check) 결과. dedupe는 composite UNIQUE로 강제 (R5-2).
CREATE TABLE scan_results (
    id              TEXT NOT NULL,                -- "scr_<ULID>" (R5-2 별도 ID)
    session_id      TEXT NOT NULL,
    tenant_id       TEXT NOT NULL,                -- 격리(중복이지만 cross-tenant 안전망)
    robot_id        TEXT NOT NULL,
    check_id        TEXT NOT NULL,                -- 팩 내 식별자 (예: "CIS-1.1.1.1")
    pack_check_id   TEXT NOT NULL,                -- pack_checks.id ("ck_<ULID>")
    outcome         TEXT NOT NULL
                        CHECK (outcome IN ('pass','fail','indeterminate','error','skipped')), -- 5-값
    eval_reason     TEXT NOT NULL DEFAULT '',
    evidence_ref    TEXT NOT NULL DEFAULT '',     -- E7 sha256 (Stage D 결선 시 채움)
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    executed_at     TEXT NOT NULL,                -- RFC3339Nano UTC
    created_at      TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (session_id) REFERENCES scan_sessions(id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (robot_id) REFERENCES robots(id),
    FOREIGN KEY (pack_check_id) REFERENCES pack_checks(id)
);

-- 같은 세션 내 (robot, check) 중복 기록 차단 (R5-2).
CREATE UNIQUE INDEX scan_results_session_robot_check
    ON scan_results(session_id, robot_id, check_id);

-- ListResults는 session_id로 모두 가져오기 + outcome 집계가 주 패턴.
CREATE INDEX scan_results_session ON scan_results(session_id);

-- +goose Down
DROP INDEX IF EXISTS scan_results_session;
DROP INDEX IF EXISTS scan_results_session_robot_check;
DROP TABLE IF EXISTS scan_results;
DROP INDEX IF EXISTS scan_sessions_tenant_status_created;
DROP INDEX IF EXISTS scan_sessions_tenant_fleet_created;
DROP TABLE IF EXISTS scan_sessions;
