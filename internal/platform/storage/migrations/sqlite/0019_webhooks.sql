-- +goose Up
-- E23 Phase 3 — Webhook + SIEM 통합 도메인 핵심 테이블.
-- 참조: docs/design/phase3-backlog.md E23
--       internal/domain/integration/webhook/README.md
--
-- 모델:
--   - webhook_endpoints: 테넌트별 등록한 외부 송출 대상 (URL + secret + event 필터).
--   - webhook_deliveries: 단일 송출 시도 1건. append-only — UPDATE는 attempt 갱신만.
--
-- 도메인 결합 (P5):
--   webhook 도메인은 audit·scan·insight 도메인을 직접 import하지 않습니다.
--   bootstrap이 EventBus 구독자를 결선하여 webhook.Service.Enqueue로 라우팅.
--
-- 멀티테넌시 (P4):
--   모든 컬럼에 tenant_id 강제. cross-tenant 조회는 application 레이어에서 격리.
--
-- 시퀀스 메모: 본 epic은 backlog에서 0020으로 가정했으나, 실제 next available은 0019.
-- E22 PostgreSQL 어댑터(0019 후보)는 본 stage 시작 시점에 미존재 — 0019로 진입.

-- webhook_endpoints: 테넌트당 등록 가능한 송출 대상.
--
-- secret은 HMAC-SHA256 서명 키 (수신자와 공유). 평문 보관 — 본 stage는 KMS 통합 없음.
-- events는 JSON 배열 (`["scan.completed", "insight.created"]`). 빈 배열은 모든 known event 구독.
-- format은 payload 직렬화 형식 enum.
CREATE TABLE webhook_endpoints (
    id          TEXT NOT NULL,                -- "wh_<ULID>"
    tenant_id   TEXT NOT NULL,
    url         TEXT NOT NULL,                -- absolute http/https URL
    secret      TEXT NOT NULL,                -- HMAC-SHA256 키 (평문)
    events      TEXT NOT NULL DEFAULT '[]',   -- JSON array of EventType strings
    format      TEXT NOT NULL DEFAULT 'json'  -- 'json'|'cef'|'ecs'
                    CHECK (format IN ('json','cef','ecs')),
    enabled     INTEGER NOT NULL DEFAULT 1    -- 0/1 boolean
                    CHECK (enabled IN (0, 1)),
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX webhook_endpoints_tenant_created
    ON webhook_endpoints(tenant_id, created_at DESC);

-- webhook_deliveries: 단일 송출 시도 1건 (append-only).
--
-- INSERT는 Enqueue 시점, UPDATE는 Process worker가 attempt_count·next_attempt_at·succeeded·last_*만 갱신.
-- 행 자체는 절대 DELETE 안 함 (P9 — audit/cross-reference용).
--
-- payload는 직렬화된 본문 (CEF/ECS/JSON 중 endpoint.format에 따른).
-- next_attempt_at은 ISO 시각 — Process worker가 `WHERE next_attempt_at <= ? AND succeeded = 0` 쿼리로 dispatch.
CREATE TABLE webhook_deliveries (
    id                   TEXT NOT NULL,                -- "whd_<ULID>"
    endpoint_id          TEXT NOT NULL,
    tenant_id            TEXT NOT NULL,                -- denormalized for cross-tenant 격리
    event_type           TEXT NOT NULL,                -- "scan.completed" 등
    event_id             TEXT NOT NULL DEFAULT '',     -- EventBus.Event.ID — cross-reference용
    payload              BLOB NOT NULL DEFAULT '',     -- 직렬화된 본문 (raw bytes)
    attempt_count        INTEGER NOT NULL DEFAULT 0,   -- 0=대기, 1~5=시도 횟수
    last_attempted_at    TEXT,                         -- NULL=시도 전
    next_attempt_at      TEXT NOT NULL,                -- ISO 시각, 0=즉시
    succeeded            INTEGER NOT NULL DEFAULT 0    -- 0/1
                            CHECK (succeeded IN (0, 1)),
    last_response_status INTEGER NOT NULL DEFAULT 0,   -- HTTP status (0=시도 전)
    last_error           TEXT NOT NULL DEFAULT '',
    created_at           TEXT NOT NULL,
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

-- +goose Down
DROP INDEX IF EXISTS webhook_deliveries_tenant_event;
DROP INDEX IF EXISTS webhook_deliveries_endpoint_created;
DROP INDEX IF EXISTS webhook_deliveries_pending;
DROP TABLE IF EXISTS webhook_deliveries;
DROP INDEX IF EXISTS webhook_endpoints_tenant_created;
DROP TABLE IF EXISTS webhook_endpoints;
