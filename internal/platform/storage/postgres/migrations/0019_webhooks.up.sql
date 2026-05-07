-- E22-B — SQLite 0019_webhooks.sql → PostgreSQL 변환.
-- 참조: docs/design/phase3-backlog.md E23
--       internal/domain/integration/webhook/README.md
--
-- 변환 메모:
--   * TEXT (JSON events) → JSONB
--   * INTEGER (boolean enabled/succeeded) → BOOLEAN (CHECK 제거 — boolean에 0/1 강제 불필요)
--   * BLOB → BYTEA (payload)
--   * TEXT (RFC3339Nano) → TIMESTAMPTZ
--   * INTEGER attempt_count/last_response_status → INTEGER 유지 (32비트 충분)

-- webhook_endpoints: 테넌트당 등록 가능한 송출 대상.
CREATE TABLE webhook_endpoints (
    id          TEXT        NOT NULL,                -- "wh_<ULID>"
    tenant_id   TEXT        NOT NULL,
    url         TEXT        NOT NULL,                -- absolute http/https URL
    secret      TEXT        NOT NULL,                -- HMAC-SHA256 키 (평문)
    events      JSONB       NOT NULL DEFAULT '[]'::jsonb,
    format      TEXT        NOT NULL DEFAULT 'json'  -- 'json'|'cef'|'ecs'
                    CHECK (format IN ('json','cef','ecs')),
    enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX webhook_endpoints_tenant_created
    ON webhook_endpoints(tenant_id, created_at DESC);

-- webhook_deliveries: 단일 송출 시도 1건 (append-only).
-- INSERT는 Enqueue 시점, UPDATE는 Process worker가 attempt_count·next_attempt_at·succeeded·last_*만 갱신.
-- 행 자체는 절대 DELETE 안 함 (P9).
CREATE TABLE webhook_deliveries (
    id                   TEXT        NOT NULL,                -- "whd_<ULID>"
    endpoint_id          TEXT        NOT NULL,
    tenant_id            TEXT        NOT NULL,
    event_type           TEXT        NOT NULL,
    event_id             TEXT        NOT NULL DEFAULT '',     -- EventBus.Event.ID
    payload              BYTEA       NOT NULL DEFAULT ''::bytea,
    attempt_count        INTEGER     NOT NULL DEFAULT 0,
    last_attempted_at    TIMESTAMPTZ,                          -- NULL=시도 전
    next_attempt_at      TIMESTAMPTZ NOT NULL,                 -- ISO 시각
    succeeded            BOOLEAN     NOT NULL DEFAULT FALSE,
    last_response_status INTEGER     NOT NULL DEFAULT 0,       -- HTTP status (0=시도 전)
    last_error           TEXT        NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (endpoint_id) REFERENCES webhook_endpoints(id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

-- 송출 대기 큐 — Process worker가 next_attempt_at·succeeded 기준으로 dispatch 대상 선별.
CREATE INDEX webhook_deliveries_pending
    ON webhook_deliveries(next_attempt_at, succeeded);

-- endpoint별 delivery 이력 — UI/admin이 조회.
CREATE INDEX webhook_deliveries_endpoint_created
    ON webhook_deliveries(endpoint_id, created_at DESC);

-- tenant 격리 + 이벤트 종류별 통계.
CREATE INDEX webhook_deliveries_tenant_event
    ON webhook_deliveries(tenant_id, event_type, created_at DESC);
