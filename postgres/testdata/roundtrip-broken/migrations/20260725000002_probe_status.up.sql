CREATE TYPE roundtrip_status AS ENUM('draft', 'live');

ALTER TABLE roundtrip_probe
ADD COLUMN status roundtrip_status NOT NULL DEFAULT 'draft';
