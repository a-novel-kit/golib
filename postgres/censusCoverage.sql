-- Expected row counts for the four PostgreSQL catalogs the census promises to cover completely.
-- census.sql tags each corresponding output row with its source catalog; comparing the totals
-- makes removing or accidentally filtering any renderer branch fail loudly.
--
-- ?0 is the schema to census.
WITH
  catalogs (catalog) AS (
    VALUES
      ('pg_class'),
      ('pg_constraint'),
      ('pg_type'),
      ('pg_proc')
  ),
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
  catalog_objects (catalog, oid) AS (
    -- Supported relations each produce one 'relation' row.
    SELECT
      'pg_class',
      c.oid
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
    -- Index relations each produce one 'index' row.
    SELECT
      'pg_class',
      i.indexrelid
    FROM
      pg_index i
      JOIN pg_class c ON c.oid = i.indexrelid
      JOIN target ON c.relnamespace = target.nsp
    UNION ALL
    -- Every remaining user-defined relation kind produces an 'unsupported' row.
    SELECT
      'pg_class',
      c.oid
    FROM
      pg_class c
      JOIN target ON c.relnamespace = target.nsp
    WHERE
      c.relkind NOT IN ('r', 'p', 'v', 'm', 'f', 'S', 'c', 'i', 'I', 't')
    UNION ALL
    SELECT
      'pg_constraint',
      con.oid
    FROM
      pg_constraint con
      JOIN target ON con.connamespace = target.nsp
    UNION ALL
    -- Supported enum, domain, and standalone composite types each produce one 'type' row.
    SELECT
      'pg_type',
      t.oid
    FROM
      pg_type t
      JOIN target ON t.typnamespace = target.nsp
    WHERE
      t.typtype IN ('e', 'd', 'c')
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
    -- Non-implied types outside that supported set each produce an 'unsupported' row.
    SELECT
      'pg_type',
      t.oid
    FROM
      pg_type t
      JOIN target ON t.typnamespace = target.nsp
    WHERE
      t.typtype NOT IN ('e', 'd', 'c')
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
    -- Regular routines produce 'routine' rows; aggregates produce 'unsupported' rows.
    SELECT
      'pg_proc',
      p.oid
    FROM
      pg_proc p
      JOIN target ON p.pronamespace = target.nsp
    WHERE
      NOT EXISTS (
        SELECT
        FROM
          extension_owned e
        WHERE
          e.objid = p.oid
          AND e.classid = 'pg_proc'::regclass
      )
  )
SELECT
  catalogs.catalog,
  count(catalog_objects.oid) AS expected
FROM
  catalogs
  LEFT JOIN catalog_objects USING (catalog)
GROUP BY
  catalogs.catalog
ORDER BY
  1;
