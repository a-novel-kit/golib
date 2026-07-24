-- The table the roundtrip fixtures seed and the later migrations extend. It exists only so the
-- harness has a schema to take apart, so it carries one of each thing a down migration has to
-- reverse: a default, a not-null, and a check.
CREATE TABLE roundtrip_probe (
  id uuid PRIMARY KEY NOT NULL DEFAULT gen_random_uuid(),
  name text NOT NULL CHECK (name <> '')
);
