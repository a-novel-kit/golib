CREATE UNIQUE INDEX roundtrip_probe_live_name_idx ON roundtrip_probe (name)
WHERE
  status = 'live';
