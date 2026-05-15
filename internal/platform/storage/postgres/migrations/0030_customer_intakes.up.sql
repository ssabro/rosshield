-- 0030_customer_intakes.up.sql — 첫 paying customer onboarding intake 테이블 (PG).
--
-- SQLite 0030 미러. design doc:
-- docs/design/notes/customer-onboarding-design.md §6.1 R1 + §7 Stage 1.
-- D-CUSTONB-1 권장 default = intake API 도입, D-CUSTONB-2 = full provisioning.
--
-- 변환 메모:
--   * SQLite TEXT → PG TEXT (변환 0).
--   * SQLite NULL 허용 → PG NULL 허용 동일.
--   * CHECK 제약은 PG도 동일 표현.

CREATE TABLE customer_intakes (
    id                     TEXT NOT NULL,
    tenant_id              TEXT NULL,
    organization_name      TEXT NOT NULL,
    primary_contact_email  TEXT NOT NULL,
    primary_contact_name   TEXT NOT NULL,
    plan_request           TEXT NOT NULL,
    intended_use           TEXT NOT NULL,
    status                 TEXT NOT NULL DEFAULT 'pending'
                               CHECK (status IN ('pending','accepted','rejected')),
    created_at             TEXT NOT NULL,
    accepted_at            TEXT NULL,
    accepted_by_user_id    TEXT NULL,
    rejected_at            TEXT NULL,
    rejection_reason       TEXT NULL,
    PRIMARY KEY (id)
);

CREATE INDEX customer_intakes_status_created ON customer_intakes(status, created_at DESC);
CREATE INDEX customer_intakes_email ON customer_intakes(primary_contact_email);
