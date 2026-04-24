-- +goose Up
-- E4 Pack 시스템 — 벤치마크 팩 + 체크 + 라이프사이클.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 BenchmarkPack
--       docs/design/07-scan-engine-and-benchmarks.md
--       docs/design/phase1-backlog.md E4

-- packs: 설치된 벤치마크 팩 메타. tenant_id='system'은 cross-tenant 공유 팩.
CREATE TABLE packs (
    id            TEXT    PRIMARY KEY, -- pk_<ULID>
    tenant_id     TEXT    NOT NULL,    -- 'system' = 전 테넌트 공유 (§4.2)
    name          TEXT    NOT NULL,    -- 'cis-ubuntu-2404'
    version       TEXT    NOT NULL,    -- semver 'v1.0.0'
    vendor        TEXT    NOT NULL,    -- 'CIS' | 'NIST' | 'fleet-internal'
    pack_key      TEXT    NOT NULL,    -- '<vendor>-<name>-<version>' 사람 친화적
    manifest_hash BLOB    NOT NULL,    -- sha256 of MANIFEST.json bytes (32B)
    signer_key_id TEXT    NOT NULL,    -- 'key_<8B hex>' — 서명 키 식별
    installed_at  TEXT    NOT NULL,    -- RFC3339Nano UTC
    UNIQUE (tenant_id, pack_key),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) -- 'system'은 별도 처리
);
CREATE INDEX packs_tenant_name ON packs(tenant_id, name);

-- pack_checks: 팩에 포함된 개별 check 정의.
-- evaluation_rule은 화이트리스트 AST(C5) JSON. expressionn 안전성은 도메인 layer에서 검증.
CREATE TABLE pack_checks (
    id              TEXT    PRIMARY KEY,            -- ck_<ULID>
    pack_id         TEXT    NOT NULL,
    check_id        TEXT    NOT NULL,               -- 'CIS-1.1.1.1' (pack 내 식별자)
    title           TEXT    NOT NULL,
    description     TEXT,
    severity        TEXT    NOT NULL DEFAULT 'medium', -- low|medium|high|critical
    audit_command   TEXT,                            -- SSH로 실행할 명령 (옵션)
    evaluation_rule TEXT    NOT NULL,                -- JSON AST {"op":"equals","value":"ok"} 등
    rationale       TEXT,
    fix_guidance    TEXT,
    UNIQUE (pack_id, check_id),
    FOREIGN KEY (pack_id) REFERENCES packs(id)
);
CREATE INDEX pack_checks_pack ON pack_checks(pack_id);

-- pack_lifecycle: 팩의 상태 전이 history (append 권장이지만 lifecycle 변경은 가변 — 별도 audit가 처리).
-- pack당 현재 상태는 latest row.
CREATE TABLE pack_lifecycle (
    pack_id      TEXT    NOT NULL,
    state        TEXT    NOT NULL, -- installed|staged|active|inactive|archived|removed
    transitioned_at TEXT NOT NULL, -- RFC3339Nano UTC
    actor_id     TEXT    NOT NULL, -- user 또는 'system'
    reason       TEXT,
    PRIMARY KEY (pack_id, transitioned_at),
    FOREIGN KEY (pack_id) REFERENCES packs(id)
);
CREATE INDEX pack_lifecycle_state ON pack_lifecycle(pack_id, state);

-- +goose Down
DROP INDEX IF EXISTS pack_lifecycle_state;
DROP TABLE IF EXISTS pack_lifecycle;
DROP INDEX IF EXISTS pack_checks_pack;
DROP TABLE IF EXISTS pack_checks;
DROP INDEX IF EXISTS packs_tenant_name;
DROP TABLE IF EXISTS packs;
