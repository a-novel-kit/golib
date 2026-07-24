-- A live row, so the partial unique index the next migration creates has to build over data that
-- matches its predicate.
INSERT INTO
  roundtrip_probe (name, status)
VALUES
  ('seeded live', 'live');
