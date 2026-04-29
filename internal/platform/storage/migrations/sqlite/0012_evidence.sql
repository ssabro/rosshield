-- +goose Up
-- E7 Stage C — Evidence Store 도메인 핵심 테이블.
-- 참조: docs/design/04-domain-and-data-model.md §4.2 EvidenceRecord
--       docs/design/07-scan-engine-and-benchmarks.md §7.8
--       docs/design/notes/e7-blobstore-research.md §1·§5
--       docs/design/notes/e7-redaction-research.md §3·§5
--       phase1-backlog.md E7
-- 결정 R9-1~R9-9 (사용자 합의 2026-04-29).

-- evidence_records: 한 평문(redact 후 sha256)에 대한 메타. blob 자체는 디스크
-- (`<dataDir>/evidence/<aa>/<bb>/<sha>.blob`)에 별도 저장 — blobstore 어댑터.
-- (tenant_id, sha256) UNIQUE — cross-tenant blob 공유 금지(R9-8). 같은 비밀이
-- 두 테넌트에 있으면 evidence_records row 2개·blob 파일 1개(blob은 hash addressing).
CREATE TABLE evidence_records (
    id              TEXT NOT NULL,                -- "ev_<ULID>"
    tenant_id       TEXT NOT NULL,
    sha256          TEXT NOT NULL,                -- 64자 lowercase hex (redact 후 평문 기준)
    content_type    TEXT NOT NULL
                        CHECK (content_type IN ('stdout','stderr','file','config-snapshot','screenshot')),
    size_bytes      INTEGER NOT NULL,             -- redact 후 평문 길이
    blob_locator    TEXT NOT NULL,                -- "fs:<sha256>" — backend prefix + key (R9-1 fs only Phase 1)
    redactions      TEXT NOT NULL DEFAULT '[]',   -- JSON 배열: [{offset,length,type}, ...]
    created_at      TEXT NOT NULL,                -- RFC3339Nano UTC
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

-- dedup 제약 + GetBySHA 검색 인덱스 둘을 한 번에.
CREATE UNIQUE INDEX evidence_records_tenant_sha
    ON evidence_records(tenant_id, sha256);

-- evidence_refs: ScanResult ↔ EvidenceRecord N:M 매핑. 한 result에 다수 evidence
-- (stdout + stderr + 추후 file 등) 가능. position은 호출 순서 보존(0부터).
-- scan_result 삭제 시 ref도 cascade — evidence_records 자체는 retention 정책에
-- 따라 별도 정리(Phase 2 GC 도구).
CREATE TABLE evidence_refs (
    scan_result_id  TEXT NOT NULL,
    evidence_id     TEXT NOT NULL,
    position        INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL,
    PRIMARY KEY (scan_result_id, evidence_id),
    FOREIGN KEY (scan_result_id) REFERENCES scan_results(id) ON DELETE CASCADE,
    FOREIGN KEY (evidence_id) REFERENCES evidence_records(id)
);

-- evidence 별 역참조 — "이 evidence가 어떤 result들에 참조되었나" 조회 (Phase 2 GC).
CREATE INDEX evidence_refs_evidence ON evidence_refs(evidence_id);

-- 이전 단일 placeholder 컬럼 정리 — N:M evidence_refs로 완전 대체(R9-7, trunk-based 깔끔히 교체).
-- SQLite 3.35.0+ (modernc.org/sqlite v1.49.x 동봉)이 ALTER TABLE DROP COLUMN 지원.
ALTER TABLE scan_results DROP COLUMN evidence_ref;

-- +goose Down
-- 복구 시 컬럼 재추가(빈 default) 후 N:M 테이블 제거.
ALTER TABLE scan_results ADD COLUMN evidence_ref TEXT NOT NULL DEFAULT '';
DROP INDEX IF EXISTS evidence_refs_evidence;
DROP TABLE IF EXISTS evidence_refs;
DROP INDEX IF EXISTS evidence_records_tenant_sha;
DROP TABLE IF EXISTS evidence_records;
