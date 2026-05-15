-- 0030_customer_intakes.down.sql — customer_intakes 롤백 (PG).

DROP INDEX IF EXISTS customer_intakes_email;
DROP INDEX IF EXISTS customer_intakes_status_created;
DROP TABLE IF EXISTS customer_intakes;
