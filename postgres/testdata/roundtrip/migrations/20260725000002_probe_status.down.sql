-- The column goes before the type it depends on.
ALTER TABLE roundtrip_probe
DROP COLUMN IF EXISTS status;

DROP TYPE IF EXISTS roundtrip_status;
