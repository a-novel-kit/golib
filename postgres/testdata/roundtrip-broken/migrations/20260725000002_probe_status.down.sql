-- Deliberately defective: the column goes, the type it introduced stays behind. This is the shape
-- of rollback bug a type-blind schema comparison passes, and the roundtrip must catch it.
ALTER TABLE roundtrip_probe
DROP COLUMN IF EXISTS status;
