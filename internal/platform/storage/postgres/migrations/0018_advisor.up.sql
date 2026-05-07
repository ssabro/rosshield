-- E22-B — SQLite 0018_advisor.sql → PostgreSQL 변환.
-- 참조: docs/design/phase2-backlog.md E16
--       docs/design/08-intelligence-and-compliance.md §8.5 Advisor
--
-- 변환 메모:
--   * TEXT (JSON args_json/result_json) → JSONB
--   * TEXT (RFC3339Nano) → TIMESTAMPTZ
--   * REAL → DOUBLE PRECISION (cost_usd)
--   * INTEGER (sequence/tokens/duration) → INTEGER 유지 (32비트 충분)

CREATE TABLE advisor_conversations (
    id          TEXT        NOT NULL,                -- "conv_<ULID>"
    tenant_id   TEXT        NOT NULL,
    user_id     TEXT        NOT NULL,
    title       TEXT        NOT NULL DEFAULT '',     -- 사용자 첫 메시지 첫 80자
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX advisor_conversations_tenant_user_updated
    ON advisor_conversations(tenant_id, user_id, updated_at DESC);

-- advisor_turns: 한 conversation 안 message 1건.
CREATE TABLE advisor_turns (
    id              TEXT             NOT NULL,            -- "turn_<ULID>"
    conversation_id TEXT             NOT NULL,
    tenant_id       TEXT             NOT NULL,            -- denormalized for cross-tenant isolation
    role            TEXT             NOT NULL
                        CHECK (role IN ('user','assistant','system','tool')),
    content         TEXT             NOT NULL DEFAULT '', -- assistant text 또는 user input
    sequence        INTEGER          NOT NULL,            -- conversation 안 순서
    llm_provider    TEXT             NOT NULL DEFAULT '',
    llm_model       TEXT             NOT NULL DEFAULT '',
    input_tokens    INTEGER          NOT NULL DEFAULT 0,
    output_tokens   INTEGER          NOT NULL DEFAULT 0,
    cost_usd        DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ      NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (conversation_id) REFERENCES advisor_conversations(id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX advisor_turns_conversation_seq
    ON advisor_turns(conversation_id, sequence);

-- advisor_tool_calls: 한 assistant turn이 호출한 read-only tool 1건.
CREATE TABLE advisor_tool_calls (
    id            TEXT        NOT NULL,                  -- "tcall_<ULID>"
    turn_id       TEXT        NOT NULL,
    tenant_id     TEXT        NOT NULL,
    tool_name     TEXT        NOT NULL,                  -- "get_check", "list_evidence", ...
    args_json     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    result_json   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    error         TEXT        NOT NULL DEFAULT '',       -- 빈 값이면 성공
    duration_ms   INTEGER     NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (turn_id) REFERENCES advisor_turns(id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX advisor_tool_calls_turn ON advisor_tool_calls(turn_id);
