CREATE TABLE roundtrip_probe (
  id uuid PRIMARY KEY NOT NULL DEFAULT gen_random_uuid(),
  name text NOT NULL CHECK (name <> '')
);
