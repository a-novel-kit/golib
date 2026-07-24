-- Seeded before the next migration adds a NOT NULL column, so that migration has to carry a
-- default rather than merely succeeding against an empty table.
INSERT INTO
  roundtrip_probe (name)
VALUES
  ('seeded before status');
