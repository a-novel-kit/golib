-- An enum plus a column that uses it. The pair is what makes the paired down migration a real
-- test: dropping the column alone leaves the type behind, and a schema comparison that cannot see
-- types would call that a clean rollback.
CREATE TYPE roundtrip_status AS ENUM('draft', 'live');

-- NOT NULL with a default, so the fixture row seeded by the previous migration is what this has to
-- survive. Without the default it would fail here, which is the point of seeding before the step.
ALTER TABLE roundtrip_probe
ADD COLUMN status roundtrip_status NOT NULL DEFAULT 'draft';
