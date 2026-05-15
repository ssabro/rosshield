-- +goose Up
-- 0030_customer_intakes.sql — 첫 paying customer onboarding intake 테이블 (R1 Stage 1).
--
-- 배경: customer onboarding 보강 R1 — design doc
-- `docs/design/notes/customer-onboarding-design.md` §6.1 R1 + §7 Stage 1 + D-CUSTONB-1/2.
--
-- 책임:
--   - 운영자(rosshield admin)가 customer-info-template.md 회수본을 등록 → 검증 → accept/reject
--     상태 머신을 영속.
--   - design doc §3.1: yaml → JSON intake API의 영속 표면 (Stage 2 handler에서 INSERT,
--     Stage 3 auto-provisioning은 본 row를 accept 시 tenant + admin invite + license token 결선).
--
-- 컬럼:
--   - tenant_id: NULL 허용 — pending/rejected 단계는 tenant 미생성. accept 트랜잭션에서
--     tenant.Create 동시 실행 후 채움 (Stage 3 결선).
--   - status: 'pending'(default) → 'accepted' | 'rejected'. CHECK 제약 강제.
--   - accepted_at·accepted_by_user_id: status='accepted' 시점에만 채움. 이전 단계는 NULL.
--   - rejected_at·rejection_reason: status='rejected' 시점에만 채움. 이전 단계는 NULL.
--
-- DDD 경계 (P5):
--   본 테이블은 intake 도메인(internal/domain/intake)에서만 R/W. cross-domain은 audit emit.
--
-- 멀티테넌시 (P4):
--   tenant_id는 accept 후 채워짐 — 본 테이블은 "tenant 생성 *전*" 단계 데이터로 NULL 허용.
--   tenant 생성된 row의 cross-tenant lookup은 application layer에서 차단.
--
-- 불변성 (P9):
--   accepted_at/rejected_at은 한 번 채워지면 코드 레벨에서 변경 금지 (sqliterepo 강제).
--   본 마이그레이션은 컬럼 NULL 허용만 — 실제 immutability는 Service 계층에서 강제.
--
-- 참조: docs/design/notes/customer-onboarding-design.md §6.1 R1 + §7 Stage 1.

CREATE TABLE customer_intakes (
    id                     TEXT NOT NULL,                                         -- "ci_<ULID>"
    tenant_id              TEXT NULL,                                             -- accept 후 채움 (FK 보류 — pending/rejected는 tenant 없음)
    organization_name      TEXT NOT NULL,
    primary_contact_email  TEXT NOT NULL,                                         -- lowercase normalize
    primary_contact_name   TEXT NOT NULL,
    plan_request           TEXT NOT NULL,                                         -- 'community' | 'pro' | 'enterprise'
    intended_use           TEXT NOT NULL,                                         -- 자유 텍스트
    status                 TEXT NOT NULL DEFAULT 'pending'
                               CHECK (status IN ('pending','accepted','rejected')),
    created_at             TEXT NOT NULL,
    accepted_at            TEXT NULL,
    accepted_by_user_id    TEXT NULL,
    rejected_at            TEXT NULL,
    rejection_reason       TEXT NULL,
    PRIMARY KEY (id)
);

-- status 별 list (운영자 admin UI: pending intake 큐).
CREATE INDEX customer_intakes_status_created ON customer_intakes(status, created_at DESC);
-- email로 중복 intake 검출 (같은 customer 재제출 추적 — UNIQUE는 아님, 운영자 결정).
CREATE INDEX customer_intakes_email ON customer_intakes(primary_contact_email);

-- +goose Down
DROP INDEX IF EXISTS customer_intakes_email;
DROP INDEX IF EXISTS customer_intakes_status_created;
DROP TABLE IF EXISTS customer_intakes;
