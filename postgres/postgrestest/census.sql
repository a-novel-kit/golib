-- Canonical (class, identity, definition) rows for one schema.
-- PostgreSQL renders definitions; identity keys make comparison order-insensitive.
-- Output is OID-free. Unsupported classes fail instead of disappearing.
--
-- ?0 is the schema to census.
WITH
  -- Extension members are represented by their owning extension.
  extension_owned AS (
    SELECT
      objid,
      classid
    FROM
      pg_depend
    WHERE
      deptype = 'e'
  ),
  target AS (
    SELECT
      oid AS nsp
    FROM
      pg_namespace
    WHERE
      nspname = ?0
  ),
  census AS (
SELECT
  'column' AS class,
  c.relname || '.' || a.attname AS identity,
  format_type(a.atttypid, a.atttypmod) || CASE WHEN a.attnotnull THEN ' NOT NULL' ELSE '' END || coalesce(' DEFAULT ' || pg_get_expr(d.adbin, d.adrelid), '') || CASE WHEN a.attidentity <> '' THEN ' IDENTITY ' || a.attidentity::text ELSE '' END || CASE WHEN a.attgenerated <> '' THEN ' GENERATED ' || a.attgenerated::text ELSE '' END || coalesce(' COLLATE ' || nullif(co.collname, 'default'), '') AS definition
FROM
  pg_attribute a
  JOIN pg_class c ON c.oid = a.attrelid
  JOIN target ON c.relnamespace = target.nsp
  LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid
  AND d.adnum = a.attnum
  LEFT JOIN pg_collation co ON co.oid = a.attcollation
WHERE
  a.attnum > 0
  AND NOT a.attisdropped
  -- Include standalone composite fields.
  AND c.relkind IN ('r', 'p', 'v', 'm', 'f', 'c')
UNION ALL
SELECT
  'relation',
  c.relname,
  c.relkind::text || coalesce(' OPTIONS ' || array_to_string(c.reloptions, ','), '') || CASE WHEN c.relrowsecurity THEN ' RLS' ELSE '' END || CASE WHEN c.relforcerowsecurity THEN ' FORCE_RLS' ELSE '' END || coalesce(' ACL ' || array_to_string(c.relacl::text[], ','), '') || coalesce(' PARTITION BY ' || pg_get_partkeydef(c.oid), '') || coalesce(' AS ' || pg_get_viewdef(c.oid, TRUE), '')
FROM
  pg_class c
  JOIN target ON c.relnamespace = target.nsp
WHERE
  c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S', 'c')
  AND NOT EXISTS (
    SELECT
    FROM
      extension_owned e
    WHERE
      e.objid = c.oid
      AND e.classid = 'pg_class'::regclass
  )
UNION ALL
SELECT
  'sequence',
  c.relname,
  s.seqtypid::regtype || ' start ' || s.seqstart || ' inc ' || s.seqincrement || ' min ' || s.seqmin || ' max ' || s.seqmax || ' cache ' || s.seqcache || CASE WHEN s.seqcycle THEN ' cycle' ELSE '' END
FROM
  pg_sequence s
  JOIN pg_class c ON c.oid = s.seqrelid
  JOIN target ON c.relnamespace = target.nsp
UNION ALL
SELECT
  'index',
  c.relname,
  pg_get_indexdef(i.indexrelid)
FROM
  pg_index i
  JOIN pg_class c ON c.oid = i.indexrelid
  JOIN target ON c.relnamespace = target.nsp
UNION ALL
SELECT
  'constraint',
  coalesce(rel.relname || '.', '') || con.conname,
  pg_get_constraintdef(con.oid)
FROM
  pg_constraint con
  JOIN target ON con.connamespace = target.nsp
  LEFT JOIN pg_class rel ON rel.oid = con.conrelid
UNION ALL
SELECT
  'type',
  t.typname,
  t.typtype::text || CASE WHEN t.typtype = 'e' THEN ' ' || coalesce(
    (
      SELECT
        string_agg(e.enumlabel, ',' ORDER BY e.enumsortorder)
      FROM
        pg_enum e
      WHERE
        e.enumtypid = t.oid
    ),
    ''
  ) WHEN t.typtype = 'd' THEN ' ' || format_type(t.typbasetype, t.typtypmod) || CASE WHEN t.typnotnull THEN ' NOT NULL' ELSE '' END || coalesce(' DEFAULT ' || t.typdefault, '') ELSE '' END
FROM
  pg_type t
  JOIN target ON t.typnamespace = target.nsp
WHERE
  t.typtype IN ('e', 'd', 'c')
  -- Table row types are implied; standalone composites (relkind 'c') are not.
  AND NOT EXISTS (
    SELECT
    FROM
      pg_class c
    WHERE
      c.reltype = t.oid
      AND c.relkind <> 'c'
  )
  AND NOT EXISTS (
    SELECT
    FROM
      extension_owned e
    WHERE
      e.objid = t.oid
      AND e.classid = 'pg_type'::regclass
  )
UNION ALL
SELECT
  'routine',
  p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')',
  -- Hash complete definitions to preserve one-line records.
  p.prokind::text || ' ' || md5(pg_get_functiondef(p.oid)) || ' ' || pg_get_function_result(p.oid) || ' ' || p.provolatile::text || ' ' || p.proisstrict::text || ' ' || p.prosecdef::text || ' ' || l.lanname
FROM
  pg_proc p
  JOIN target ON p.pronamespace = target.nsp
  JOIN pg_language l ON l.oid = p.prolang
WHERE
  p.prokind <> 'a'
  AND NOT EXISTS (
    SELECT
    FROM
      extension_owned e
    WHERE
      e.objid = p.oid
      AND e.classid = 'pg_proc'::regclass
  )
UNION ALL
SELECT
  'trigger',
  c.relname || '.' || tg.tgname,
  pg_get_triggerdef(tg.oid) || ' ENABLED ' || tg.tgenabled::text
FROM
  pg_trigger tg
  JOIN pg_class c ON c.oid = tg.tgrelid
  JOIN target ON c.relnamespace = target.nsp
WHERE
  NOT tg.tgisinternal
UNION ALL
SELECT
  'policy',
  c.relname || '.' || pol.polname,
  pol.polcmd::text || ' ' || pol.polpermissive::text || ' ' || array_to_string(
    ARRAY(
      SELECT
        CASE WHEN role_oid = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(role_oid) END
      FROM
        unnest(pol.polroles) AS roles (role_oid)
      ORDER BY
        1
    ),
    ','
  ) || ' ' || coalesce(pg_get_expr(pol.polqual, pol.polrelid), '-') || ' ' || coalesce(pg_get_expr(pol.polwithcheck, pol.polrelid), '-')
FROM
  pg_policy pol
  JOIN pg_class c ON c.oid = pol.polrelid
  JOIN target ON c.relnamespace = target.nsp
UNION ALL
SELECT
  'rule',
  c.relname || '.' || r.rulename,
  pg_get_ruledef(r.oid)
FROM
  pg_rewrite r
  JOIN pg_class c ON c.oid = r.ev_class
  JOIN target ON c.relnamespace = target.nsp
WHERE
  -- Every view owns a _RETURN rule, already covered by the view's own definition.
  r.rulename <> '_RETURN'
UNION ALL
SELECT
  'statistics',
  st.stxname,
  pg_get_statisticsobjdef(st.oid)
FROM
  pg_statistic_ext st
  JOIN target ON st.stxnamespace = target.nsp
UNION ALL
SELECT
  'collation',
  col.collname,
  col.collprovider::text || ' ' || coalesce(col.collcollate, '-') || ' ' || coalesce(col.collctype, '-')
FROM
  pg_collation col
  JOIN target ON col.collnamespace = target.nsp
UNION ALL
SELECT
  'extension',
  e.extname,
  e.extversion
FROM
  pg_extension e
UNION ALL
SELECT
  'schema',
  n.nspname,
  coalesce(array_to_string(n.nspacl::text[], ','), '')
FROM
  pg_namespace n
WHERE
  n.nspname NOT LIKE 'pg\_%'
  AND n.nspname <> 'information_schema'
  -- Comments are censused by the name of what they hang off, never by OID.
UNION ALL
SELECT
  'comment',
  c.relname || coalesce('.' || a.attname, ''),
  d.description
FROM
  pg_description d
  JOIN pg_class c ON c.oid = d.objoid
  JOIN target ON c.relnamespace = target.nsp
  LEFT JOIN pg_attribute a ON a.attrelid = d.objoid
  AND a.attnum = d.objsubid
UNION ALL
SELECT
  'comment',
  'type ' || t.typname,
  d.description
FROM
  pg_description d
  JOIN pg_type t ON t.oid = d.objoid
  JOIN target ON t.typnamespace = target.nsp
UNION ALL
SELECT
  'comment',
  'routine ' || p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')',
  d.description
FROM
  pg_description d
  JOIN pg_proc p ON p.oid = d.objoid
  JOIN target ON p.pronamespace = target.nsp
UNION ALL
SELECT
  'comment',
  'constraint ' || coalesce(rel.relname || '.', '') || con.conname,
  d.description
FROM
  pg_description d
  JOIN pg_constraint con ON con.oid = d.objoid
  JOIN target ON con.connamespace = target.nsp
  LEFT JOIN pg_class rel ON rel.oid = con.conrelid
UNION ALL
SELECT
  'comment',
  'schema ' || n.nspname,
  d.description
FROM
  pg_description d
  JOIN pg_namespace n ON n.oid = d.objoid
WHERE
  n.nspname NOT LIKE 'pg\_%'
  AND n.nspname <> 'information_schema'
  -- Unsupported objects fail the snapshot instead of disappearing.
UNION ALL
SELECT
  'unsupported',
  'relkind ' || c.relkind::text,
  c.relname
FROM
  pg_class c
  JOIN target ON c.relnamespace = target.nsp
WHERE
  -- 'i' and 'I' are censused through pg_index, 't' is an internal TOAST table.
  c.relkind NOT IN ('r', 'p', 'v', 'm', 'f', 'S', 'c', 'i', 'I', 't')
UNION ALL
SELECT
  'unsupported',
  'typtype ' || t.typtype::text,
  t.typname
FROM
  pg_type t
  JOIN target ON t.typnamespace = target.nsp
WHERE
  t.typtype NOT IN ('e', 'd', 'c')
  -- An array type is implied by its element type, a row type by its relation.
  AND t.typcategory <> 'A'
  AND NOT EXISTS (
    SELECT
    FROM
      pg_class c
    WHERE
      c.reltype = t.oid
  )
  AND NOT EXISTS (
    SELECT
    FROM
      extension_owned e
    WHERE
      e.objid = t.oid
      AND e.classid = 'pg_type'::regclass
  )
UNION ALL
SELECT
  'unsupported',
  'aggregate',
  p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')'
FROM
  pg_proc p
  JOIN target ON p.pronamespace = target.nsp
WHERE
  -- An aggregate's behaviour lives in pg_aggregate, not in the prosrc the routine class hashes.
  p.prokind = 'a'
  AND NOT EXISTS (
    SELECT
    FROM
      extension_owned e
    WHERE
      e.objid = p.oid
      AND e.classid = 'pg_proc'::regclass
  )
UNION ALL
SELECT
  'unsupported',
  'operator',
  op.oprname
FROM
  pg_operator op
  JOIN target ON op.oprnamespace = target.nsp
WHERE
  NOT EXISTS (
    SELECT
    FROM
      extension_owned e
    WHERE
      e.objid = op.oid
      AND e.classid = 'pg_operator'::regclass
  )
UNION ALL
SELECT
  'unsupported',
  'operator class',
  opc.opcname
FROM
  pg_opclass opc
  JOIN target ON opc.opcnamespace = target.nsp
WHERE
  NOT EXISTS (
    SELECT
    FROM
      extension_owned e
    WHERE
      e.objid = opc.oid
      AND e.classid = 'pg_opclass'::regclass
  )
UNION ALL
SELECT
  'unsupported',
  'text search configuration',
  cfg.cfgname
FROM
  pg_ts_config cfg
  JOIN target ON cfg.cfgnamespace = target.nsp
WHERE
  NOT EXISTS (
    SELECT
    FROM
      extension_owned e
    WHERE
      e.objid = cfg.oid
      AND e.classid = 'pg_ts_config'::regclass
  )
UNION ALL
SELECT
  'unsupported',
  'conversion',
  conv.conname
FROM
  pg_conversion conv
  JOIN target ON conv.connamespace = target.nsp
WHERE
  NOT EXISTS (
    SELECT
    FROM
      extension_owned e
    WHERE
      e.objid = conv.oid
      AND e.classid = 'pg_conversion'::regclass
  )
  )
SELECT
  CASE
    WHEN class IN ('relation', 'index') THEN 'pg_class'
    WHEN class = 'constraint' THEN 'pg_constraint'
    WHEN class = 'type' THEN 'pg_type'
    WHEN class = 'routine' THEN 'pg_proc'
    WHEN class = 'unsupported' AND identity LIKE 'relkind %' THEN 'pg_class'
    WHEN class = 'unsupported' AND identity LIKE 'typtype %' THEN 'pg_type'
    WHEN class = 'unsupported' AND identity = 'aggregate' THEN 'pg_proc'
    ELSE ''
  END AS catalog,
  class,
  identity,
  definition
FROM
  census
ORDER BY
  2,
  3,
  4;
