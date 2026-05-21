-- Phase 10.D-4 — audit_entries.key_epoch 컬럼 추가.
--
-- design: docs/design/notes/audit-chain-rotation-automation-design.md §6.4 + §12.1 Stage 10.D-4.
--
-- 의미:
--   - 각 audit entry 가 INSERT 된 시점의 활성 signer epoch 를 보존.
--   - NULL  → epoch 인식 없이 INSERT 된 row (마이그레이션 이전 / SwappableSigner 미주입).
--   - 양수  → SwappableSigner 활성 epoch (audit_chain_keys.epoch 와 일치).
--
-- 외부 감사인은 추후 entry 단위 epoch + audit_chain_keys 의 epoch 별 public key 매칭으로
-- 검증 가능 (직접 entry 서명은 없으나 checkpoint 와 함께 활성 epoch trace 제공).
--
-- 호환성: nullable + default NULL — 기존 row 영향 0.

ALTER TABLE audit_entries ADD COLUMN key_epoch BIGINT;
