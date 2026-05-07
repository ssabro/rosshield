-- E22-B — SQLite 0011_scan.sql → PostgreSQL 변환.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 ScanSession/ScanResult
--       docs/design/07-scan-engine-and-benchmarks.md §7.2·§7.3
--
-- 변환 메모:
--   * TEXT (RFC3339Nano) → TIMESTAMPTZ
--   * CHECK 제약은 그대로 (PG 표준)
--   * 인덱스(DESC 포함)는 그대로 (PG 지원)

-- scan_sessions: 스캔 실행 단위. FSM = pending → running → {completed | failed | cancelled}.
CREATE TABLE scan_sessions (
    id                  TEXT        NOT NULL,
    tenant_id           TEXT        NOT NULL,
    fleet_id            TEXT        NOT NULL,
    pack_id             TEXT        NOT NULL,
    trigger             TEXT        NOT NULL DEFAULT 'manual'
                            CHECK (trigger IN ('manual','schedule','event')),
    status              TEXT        NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending','running','completed','failed','cancelled')),
    progress_total      INTEGER     NOT NULL DEFAULT 0,
    progress_completed  INTEGER     NOT NULL DEFAULT 0,
    progress_failed     INTEGER     NOT NULL DEFAULT 0,
    failure_reason      TEXT        NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    started_at          TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (fleet_id) REFERENCES fleets(id),
    FOREIGN KEY (pack_id) REFERENCES packs(id)
);

CREATE INDEX scan_sessions_tenant_fleet_created
    ON scan_sessions(tenant_id, fleet_id, created_at DESC);
CREATE INDEX scan_sessions_tenant_status_created
    ON scan_sessions(tenant_id, status, created_at DESC);

-- scan_results: 세션 내 (robot × check) 결과.
CREATE TABLE scan_results (
    id              TEXT        NOT NULL,
    session_id      TEXT        NOT NULL,
    tenant_id       TEXT        NOT NULL,
    robot_id        TEXT        NOT NULL,
    check_id        TEXT        NOT NULL,
    pack_check_id   TEXT        NOT NULL,
    outcome         TEXT        NOT NULL
                        CHECK (outcome IN ('pass','fail','indeterminate','error','skipped')),
    eval_reason     TEXT        NOT NULL DEFAULT '',
    evidence_ref    TEXT        NOT NULL DEFAULT '',     -- 0012에서 DROP 됨 (E7 N:M)
    duration_ms     INTEGER     NOT NULL DEFAULT 0,
    executed_at     TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (session_id) REFERENCES scan_sessions(id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (robot_id) REFERENCES robots(id),
    FOREIGN KEY (pack_check_id) REFERENCES pack_checks(id)
);

CREATE UNIQUE INDEX scan_results_session_robot_check
    ON scan_results(session_id, robot_id, check_id);

CREATE INDEX scan_results_session ON scan_results(session_id);
