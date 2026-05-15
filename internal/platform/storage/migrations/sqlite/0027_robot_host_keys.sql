-- +goose Up
-- 0027_robot_host_keys.sql — TOFU host key trust 테이블.
--
-- 배경: scanrun SSH 통합 Stage 1 — design doc `scanrun-ssh-integration-design.md`
-- §5.5 + Stage 1. 첫 SSH 접속 시 first-touch trust(TOFU) + 변경 즉시 차단을
-- 위해 robot별 trusted host key fingerprint를 영속.
--
-- 정책 (D-SCAN-2 권장 default = TOFU):
--   - 첫 호출은 RecordFirstTouch — 즉시 trusted 상태로 INSERT
--   - 두 번째 이후는 GetTrustedKey로 비교, 일치하면 pass / 불일치 즉시 ErrHostKeyMismatch
--   - 운영자 명시 ResetTrust 시 trust_state='revoked'로 marking — 다음 호출이 first-touch처럼 동작
--
-- tenant_id는 NOT NULL — 원칙 §4 멀티테넌시 기본값. UNIQUE는 (tenant_id, robot_id, fingerprint_sha256)
-- 으로 같은 robot이라도 fingerprint가 다르면 별 row(history 보존), 같은 fingerprint 중복 삽입 방지.
--
-- 참조: docs/design/notes/scanrun-ssh-integration-design.md §5.5 + §6 Stage 1.

CREATE TABLE robot_host_keys (
    id                   TEXT NOT NULL,
    tenant_id            TEXT NOT NULL,
    robot_id             TEXT NOT NULL,
    fingerprint_sha256   TEXT NOT NULL, -- 'SHA256:<base64-no-pad>' 형식 (OpenSSH 표준)
    key_type             TEXT NOT NULL, -- 'ssh-rsa' | 'ssh-ed25519' | 'ecdsa-sha2-nistp256' 등
    key_blob             BLOB NOT NULL, -- ssh.PublicKey.Marshal() 결과 (재구성용)
    first_seen_at        TEXT NOT NULL,
    last_verified_at     TEXT NOT NULL,
    trust_state          TEXT NOT NULL DEFAULT 'trusted', -- 'trusted' | 'revoked'
    PRIMARY KEY (id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (robot_id) REFERENCES robots(id)
);

CREATE INDEX robot_host_keys_tenant_robot ON robot_host_keys(tenant_id, robot_id);

-- partial unique: 같은 (tenant, robot, fingerprint) 중복 삽입 차단.
-- robot당 다중 키는 history로 허용(과거 trusted였다가 revoked된 키 + 새 trusted 키 공존).
CREATE UNIQUE INDEX robot_host_keys_unique
    ON robot_host_keys(tenant_id, robot_id, fingerprint_sha256);

-- +goose Down
DROP INDEX IF EXISTS robot_host_keys_unique;
DROP INDEX IF EXISTS robot_host_keys_tenant_robot;
DROP TABLE IF EXISTS robot_host_keys;
