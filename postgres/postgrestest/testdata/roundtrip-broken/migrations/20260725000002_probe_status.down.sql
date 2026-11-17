-- Intentionally leaves roundtrip_status behind.
ALTER TABLE roundtrip_probe
DROP COLUMN IF EXISTS status;
