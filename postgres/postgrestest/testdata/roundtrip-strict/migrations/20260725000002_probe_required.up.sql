-- Intentionally fails only when the previous fixture inserted a row.
ALTER TABLE roundtrip_probe
ADD COLUMN required text NOT NULL;
