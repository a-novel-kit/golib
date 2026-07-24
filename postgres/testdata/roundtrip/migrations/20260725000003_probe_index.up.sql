-- A partial unique index, so the roundtrip covers a predicate a rollback could restore too widely.
CREATE UNIQUE INDEX roundtrip_probe_live_name_idx ON roundtrip_probe (name)
WHERE
  status = 'live';
