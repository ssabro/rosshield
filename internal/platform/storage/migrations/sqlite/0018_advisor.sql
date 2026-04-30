-- +goose Up
-- E16 Phase 2 — Advisor 대화 오케스트레이터.
-- 참조: docs/design/phase2-backlog.md E16
--       docs/design/08-intelligence-and-compliance.md §8.5 Advisor (대화형)
--
-- 모델:
--   - advisor_conversations: tenant·user 단위 대화 1건 (제목·생성/갱신 시점).
--   - advisor_turns: 대화 안 message 1건 (role: user|assistant|system|tool).
--   - advisor_tool_calls: assistant turn이 호출한 read-only tool 1건 (audit cross-check용).
--
-- 도메인 결합 (P5):
--   advisor 도메인은 read-only tool을 통해서만 다른 도메인 read 호출 — write 절대 금지.
--   tool dispatch 결과는 redaction(E7) 거친 후 prompt에 포함.

CREATE TABLE advisor_conversations (
    id          TEXT NOT NULL,                -- "conv_<ULID>"
    tenant_id   TEXT NOT NULL,
    user_id     TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',     -- 사용자 첫 메시지 첫 80자 자동 생성
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX advisor_conversations_tenant_user_updated
    ON advisor_conversations(tenant_id, user_id, updated_at DESC);

-- advisor_turns: 한 conversation 안 message 1건. role enum:
--   user      : 사용자 입력
--   assistant : LLM 응답 (tool_calls 포함 가능)
--   system    : system prompt (선택)
--   tool      : tool 호출 결과 (assistant.tool_use에 대한 응답)
CREATE TABLE advisor_turns (
    id              TEXT NOT NULL,            -- "turn_<ULID>"
    conversation_id TEXT NOT NULL,
    tenant_id       TEXT NOT NULL,            -- denormalized for cross-tenant isolation
    role            TEXT NOT NULL
                        CHECK (role IN ('user','assistant','system','tool')),
    content         TEXT NOT NULL DEFAULT '', -- assistant text 본문 또는 user input
    sequence        INTEGER NOT NULL,         -- conversation 안 순서 (0부터 증가)
    llm_provider    TEXT NOT NULL DEFAULT '',
    llm_model       TEXT NOT NULL DEFAULT '',
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    cost_usd        REAL NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (conversation_id) REFERENCES advisor_conversations(id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX advisor_turns_conversation_seq
    ON advisor_turns(conversation_id, sequence);

-- advisor_tool_calls: 한 assistant turn이 호출한 read-only tool 1건.
-- ResultJSON은 redaction(E7) 거친 후 영속 — raw 자격 증명·secret 제거.
CREATE TABLE advisor_tool_calls (
    id            TEXT NOT NULL,              -- "tcall_<ULID>"
    turn_id       TEXT NOT NULL,
    tenant_id     TEXT NOT NULL,
    tool_name     TEXT NOT NULL,              -- "get_check", "list_evidence", ...
    args_json     TEXT NOT NULL DEFAULT '{}', -- JSON 직렬화 입력
    result_json   TEXT NOT NULL DEFAULT '{}', -- JSON 직렬화 결과 (redacted)
    error         TEXT NOT NULL DEFAULT '',   -- 빈 값이면 성공
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (turn_id) REFERENCES advisor_turns(id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

CREATE INDEX advisor_tool_calls_turn ON advisor_tool_calls(turn_id);

-- +goose Down
DROP INDEX IF EXISTS advisor_tool_calls_turn;
DROP TABLE IF EXISTS advisor_tool_calls;
DROP INDEX IF EXISTS advisor_turns_conversation_seq;
DROP TABLE IF EXISTS advisor_turns;
DROP INDEX IF EXISTS advisor_conversations_tenant_user_updated;
DROP TABLE IF EXISTS advisor_conversations;
