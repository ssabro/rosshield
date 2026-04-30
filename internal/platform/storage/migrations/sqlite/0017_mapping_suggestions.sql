-- +goose Up
-- E17 Phase 2 — LLM 자동 매핑 제안 영속.
-- 참조: docs/design/phase2-backlog.md E17
--       docs/design/08-intelligence-and-compliance.md §8.5 LLM mapper
--
-- 자동 적용 금지(R14-1·P2 옵트인) — suggestion은 영속만 하고 사용자가 confirm 시
-- ControlDefinition.MappedCheckIDs는 별도 흐름으로 갱신(현재 Phase 2는 git commit YAML 변경 → 재배포).
-- 본 테이블은 감사 가능한 의사결정 기록 + 사용자 검토 큐 역할.

CREATE TABLE mapping_suggestions (
    id              TEXT NOT NULL,                -- "ms_<ULID>"
    tenant_id       TEXT NOT NULL,
    check_code      TEXT NOT NULL,                -- pack 내 check.code (예: "CIS-1.1.1.1")
    framework       TEXT NOT NULL,                -- 제안 대상 framework (예: "isms-p")
    control_id      TEXT NOT NULL,                -- 제안 control ID (예: "ISMS-P:2.5.1")
    confidence      REAL NOT NULL DEFAULT 0.0,    -- 0.0~1.0 (LLM 추정)
    reasoning       TEXT NOT NULL DEFAULT '',     -- LLM이 생성한 rationale (P11 explainability)
    produced_by     TEXT NOT NULL DEFAULT 'llm'   -- 'llm'|'rules'|'manual' — 미래 다른 출처 대비
                        CHECK (produced_by IN ('llm', 'rules', 'manual')),
    status          TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'confirmed', 'rejected')),
    llm_provider    TEXT NOT NULL DEFAULT '',     -- "ollama"|"anthropic"|"noop" (LlmTrace.Provider)
    llm_model       TEXT NOT NULL DEFAULT '',     -- "claude-3-haiku-20240307" 등
    created_at      TEXT NOT NULL,                -- RFC3339Nano UTC
    decided_at      TEXT,                         -- confirm/reject 전이 시점
    decided_by      TEXT,                         -- userID 또는 'system'
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    -- 같은 tenant·check·control 조합 중복은 UNIQUE — 같은 조합으로 두 번 제안되지 않게.
    UNIQUE (tenant_id, check_code, control_id)
);

-- 활성 pending 큐: 사용자 검토 화면이 가장 자주 조회.
CREATE INDEX mapping_suggestions_pending
    ON mapping_suggestions(tenant_id, created_at DESC) WHERE status = 'pending';

-- check_code별 history (한 check 의 후보 control 비교용).
CREATE INDEX mapping_suggestions_check
    ON mapping_suggestions(tenant_id, check_code, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS mapping_suggestions_check;
DROP INDEX IF EXISTS mapping_suggestions_pending;
DROP TABLE IF EXISTS mapping_suggestions;
