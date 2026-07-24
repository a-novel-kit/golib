-- Deliberately missing a default. On an empty table this succeeds; on the row the previous
-- migration's fixture seeded it cannot. That difference is the whole reason fixtures are applied
-- between steps rather than after the last one.
ALTER TABLE roundtrip_probe
ADD COLUMN required text NOT NULL;
