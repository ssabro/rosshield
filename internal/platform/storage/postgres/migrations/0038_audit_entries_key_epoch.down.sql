-- Phase 10.D-4 회수 — audit_entries.key_epoch 제거.

ALTER TABLE audit_entries DROP COLUMN key_epoch;
