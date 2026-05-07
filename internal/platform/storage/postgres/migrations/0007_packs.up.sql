-- E22-B — SQLite 0007_packs.sql → PostgreSQL 변환.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 BenchmarkPack
--       docs/design/07-scan-engine-and-benchmarks.md
--
-- 변환 메모:
--   * BLOB → BYTEA (manifest_hash)
--   * TEXT (RFC3339Nano) → TIMESTAMPTZ
--   * TEXT (JSON AST) → JSONB (evaluation_rule)

-- packs: 설치된 벤치마크 팩 메타. tenant_id='system'은 cross-tenant 공유 팩.
CREATE TABLE packs (
    id            TEXT        PRIMARY KEY, -- pk_<ULID>
    tenant_id     TEXT        NOT NULL,    -- 'system' = 전 테넌트 공유
    name          TEXT        NOT NULL,    -- 'cis-ubuntu-2404'
    version       TEXT        NOT NULL,    -- semver 'v1.0.0'
    vendor        TEXT        NOT NULL,    -- 'CIS' | 'NIST' | 'fleet-internal'
    pack_key      TEXT        NOT NULL,    -- '<vendor>-<name>-<version>'
    manifest_hash BYTEA       NOT NULL,    -- sha256 of MANIFEST.json bytes (32B)
    signer_key_id TEXT        NOT NULL,    -- 'key_<8B hex>'
    installed_at  TIMESTAMPTZ NOT NULL,
    UNIQUE (tenant_id, pack_key),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) -- 'system'은 별도 처리
);
CREATE INDEX packs_tenant_name ON packs(tenant_id, name);

-- pack_checks: 팩에 포함된 개별 check 정의.
CREATE TABLE pack_checks (
    id              TEXT  PRIMARY KEY,                  -- ck_<ULID>
    pack_id         TEXT  NOT NULL,
    check_id        TEXT  NOT NULL,                     -- 'CIS-1.1.1.1' (pack 내 식별자)
    title           TEXT  NOT NULL,
    description     TEXT,
    severity        TEXT  NOT NULL DEFAULT 'medium',    -- low|medium|high|critical
    audit_command   TEXT,                                -- SSH로 실행할 명령 (옵션)
    evaluation_rule JSONB NOT NULL,                     -- AST {"op":"equals","value":"ok"} 등
    rationale       TEXT,
    fix_guidance    TEXT,
    UNIQUE (pack_id, check_id),
    FOREIGN KEY (pack_id) REFERENCES packs(id)
);
CREATE INDEX pack_checks_pack ON pack_checks(pack_id);

-- pack_lifecycle: 팩의 상태 전이 history.
CREATE TABLE pack_lifecycle (
    pack_id         TEXT        NOT NULL,
    state           TEXT        NOT NULL, -- installed|staged|active|inactive|archived|removed
    transitioned_at TIMESTAMPTZ NOT NULL,
    actor_id        TEXT        NOT NULL, -- user 또는 'system'
    reason          TEXT,
    PRIMARY KEY (pack_id, transitioned_at),
    FOREIGN KEY (pack_id) REFERENCES packs(id)
);
CREATE INDEX pack_lifecycle_state ON pack_lifecycle(pack_id, state);
