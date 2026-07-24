package postgres_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/a-novel-kit/golib/postgres"
)

// Use separate databases because extensions are database-scoped.
func probeDB(t *testing.T, statements ...string) *bun.DB {
	t.Helper()

	dsn := os.Getenv("POSTGRES_DSN")
	require.NotEmpty(t, dsn,
		"POSTGRES_DSN must point at a throwaway database — the census reads a real catalog")

	name := "census_probe_" + strings.ToLower(rand.Text())

	open := func(options ...pgdriver.Option) *bun.DB {
		return bun.NewDB(sql.OpenDB(pgdriver.NewConnector(options...)), pgdialect.New())
	}

	maintenance := open(pgdriver.WithDSN(dsn))

	ctx := t.Context()

	_, err := maintenance.NewRaw("CREATE DATABASE ?", bun.Ident(name)).Exec(ctx)
	require.NoError(t, err, "create the probe database")

	t.Cleanup(func() {
		// t.Context is already cancelled by the time cleanups run.
		cleanup := open(pgdriver.WithDSN(dsn))
		defer func() { _ = cleanup.Close() }()

		_, _ = cleanup.NewRaw(
			"DROP DATABASE IF EXISTS ? WITH (FORCE)", bun.Ident(name),
		).Exec(context.Background())
	})

	require.NoError(t, maintenance.Close())

	db := open(pgdriver.WithDSN(dsn), pgdriver.WithDatabase(name))

	t.Cleanup(func() { _ = db.Close() })

	for _, statement := range statements {
		_, err = db.NewRaw(statement).Exec(ctx)
		require.NoErrorf(t, err, "probe statement %q", statement)
	}

	return db
}

func probeSnapshot(t *testing.T, statements ...string) string {
	t.Helper()

	snapshot, err := postgres.SchemaSnapshot(t.Context(), probeDB(t, statements...), "public")
	require.NoError(t, err)

	return snapshot
}

type drift struct {
	name string
	from []string
	to   []string
}

// Each case is schema drift a rollback must expose.
func TestSchemaSnapshotReportsDrift(t *testing.T) {
	t.Parallel()

	cases := []drift{
		{
			name: "table storage parameters not reset",
			from: []string{`CREATE TABLE t (a int)`, `ALTER TABLE t SET (autovacuum_vacuum_scale_factor = 0.02)`},
			to:   []string{`CREATE TABLE t (a int)`},
		},
		{
			name: "NOT NULL lost",
			from: []string{`CREATE TABLE t (a int NOT NULL)`},
			to:   []string{`CREATE TABLE t (a int)`},
		},
		{
			name: "column default lost",
			from: []string{`CREATE TABLE t (a int NOT NULL DEFAULT 4)`},
			to:   []string{`CREATE TABLE t (a int NOT NULL)`},
		},
		{
			name: "index predicate widened",
			from: []string{`CREATE TABLE t (a int, s text)`, `CREATE INDEX i ON t (a) WHERE s = 'x'`},
			to:   []string{`CREATE TABLE t (a int, s text)`, `CREATE INDEX i ON t (a)`},
		},
		{
			name: "index sort order lost",
			from: []string{`CREATE TABLE t (a int)`, `CREATE INDEX i ON t (a DESC)`},
			to:   []string{`CREATE TABLE t (a int)`, `CREATE INDEX i ON t (a)`},
		},
		{
			name: "column comment lost",
			from: []string{`CREATE TABLE t (a int)`, `COMMENT ON COLUMN t.a IS 'why'`},
			to:   []string{`CREATE TABLE t (a int)`},
		},
		{
			name: "enum label order changed",
			from: []string{`CREATE TYPE e AS ENUM ('a', 'b')`},
			to:   []string{`CREATE TYPE e AS ENUM ('b', 'a')`},
		},
		{
			name: "enum value left behind",
			from: []string{`CREATE TYPE e AS ENUM ('a', 'b')`},
			to:   []string{`CREATE TYPE e AS ENUM ('a', 'b', 'c')`},
		},
		{
			name: "foreign key action changed",
			from: []string{`CREATE TABLE p (id int PRIMARY KEY)`, `CREATE TABLE t (p int REFERENCES p (id) ON DELETE CASCADE)`},
			to:   []string{`CREATE TABLE p (id int PRIMARY KEY)`, `CREATE TABLE t (p int REFERENCES p (id))`},
		},
		{
			name: "function body changed",
			from: []string{`CREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$`},
			to:   []string{`CREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$ SELECT 2 $$`},
		},
		{
			name: "function setting changed",
			from: []string{`CREATE FUNCTION f() RETURNS int LANGUAGE sql SET search_path = public AS $$ SELECT 1 $$`},
			to:   []string{`CREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$`},
		},
		{
			name: "trigger left behind",
			from: []string{`CREATE TABLE t (a int)`},
			to: []string{
				`CREATE TABLE t (a int)`,
				`CREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`,
				`CREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION f()`,
			},
		},
		{
			name: "trigger disabled",
			from: []string{
				`CREATE TABLE t (a int)`,
				`CREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`,
				`CREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION f()`,
			},
			to: []string{
				`CREATE TABLE t (a int)`,
				`CREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`,
				`CREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION f()`,
				`ALTER TABLE t DISABLE TRIGGER tr`,
			},
		},
		{
			name: "view left behind",
			from: []string{`CREATE TABLE t (a int)`},
			to:   []string{`CREATE TABLE t (a int)`, `CREATE VIEW v AS SELECT a FROM t`},
		},
		{
			name: "materialized view left behind",
			from: []string{`CREATE TABLE t (a int)`},
			to:   []string{`CREATE TABLE t (a int)`, `CREATE MATERIALIZED VIEW mv AS SELECT a FROM t`},
		},
		{
			name: "sequence left behind",
			from: []string{`CREATE TABLE t (a int)`},
			to:   []string{`CREATE TABLE t (a int)`, `CREATE SEQUENCE s`},
		},
		{
			name: "extension left behind",
			from: []string{`CREATE TABLE t (a int)`},
			to:   []string{`CREATE TABLE t (a int)`, `CREATE EXTENSION IF NOT EXISTS pg_trgm`},
		},
		{
			name: "row level security left enabled",
			from: []string{`CREATE TABLE t (a int)`},
			to: []string{
				`CREATE TABLE t (a int)`,
				`ALTER TABLE t ENABLE ROW LEVEL SECURITY`,
				`CREATE POLICY p ON t USING (a > 0)`,
			},
		},
		{
			name: "row level security policy role changed",
			from: []string{
				`CREATE TABLE t (a int)`,
				`ALTER TABLE t ENABLE ROW LEVEL SECURITY`,
				`CREATE POLICY p ON t TO PUBLIC USING (a > 0)`,
			},
			to: []string{
				`CREATE TABLE t (a int)`,
				`ALTER TABLE t ENABLE ROW LEVEL SECURITY`,
				`CREATE POLICY p ON t TO pg_read_all_data USING (a > 0)`,
			},
		},
		{
			name: "grant left behind",
			from: []string{`CREATE TABLE t (a int)`},
			to:   []string{`CREATE TABLE t (a int)`, `GRANT SELECT ON t TO PUBLIC`},
		},
		{
			name: "extended statistics left behind",
			from: []string{`CREATE TABLE t (a int, b int)`},
			to:   []string{`CREATE TABLE t (a int, b int)`, `CREATE STATISTICS st ON a, b FROM t`},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			delta := postgres.SnapshotDelta(
				probeSnapshot(t, testCase.from...),
				probeSnapshot(t, testCase.to...),
			)
			require.NotEmpty(t, delta, "the census must report this difference")
		})
	}
}

// Each case is equivalent schema a textual dump can report as different.
func TestSchemaSnapshotIgnoresNonDifferences(t *testing.T) {
	t.Parallel()

	cases := []drift{
		{
			// PostgreSQL appends a re-added column; ordinal position is not schema drift.
			name: "column re-added at the end by a rollback",
			from: []string{`CREATE TABLE t (a int, b int, c int)`},
			to: []string{
				`CREATE TABLE t (a int, b int, c int)`,
				`ALTER TABLE t DROP COLUMN b`,
				`ALTER TABLE t ADD COLUMN b int`,
			},
		},
		{
			name: "tables created in a different order",
			from: []string{`CREATE TABLE b (x int)`, `CREATE TABLE a (x int)`},
			to:   []string{`CREATE TABLE a (x int)`, `CREATE TABLE b (x int)`},
		},
		{
			name: "rows present",
			from: []string{`CREATE TABLE t (a int)`},
			to:   []string{`CREATE TABLE t (a int)`, `INSERT INTO t VALUES (1)`},
		},
		{
			name: "constraint spelled inline versus named",
			from: []string{`CREATE TABLE t (a int PRIMARY KEY)`},
			to:   []string{`CREATE TABLE t (a int)`, `ALTER TABLE t ADD PRIMARY KEY (a)`},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			delta := postgres.SnapshotDelta(
				probeSnapshot(t, testCase.from...),
				probeSnapshot(t, testCase.to...),
			)
			require.Empty(t, delta, "the census must treat these schemas as identical")
		})
	}
}

// Opposite creation order detects leaked OIDs.
func TestSchemaSnapshotIsIndependentOfCreationOrder(t *testing.T) {
	t.Parallel()

	first := []string{
		`CREATE TYPE st AS ENUM ('a', 'b')`,
		`CREATE TABLE t (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), s st NOT NULL)`,
		`CREATE UNIQUE INDEX ti ON t (s) WHERE s = 'a'`,
	}
	second := []string{
		`CREATE TABLE other (id int PRIMARY KEY)`,
		`CREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$`,
		`CREATE SEQUENCE sq`,
	}
	comments := []string{
		`COMMENT ON TABLE t IS 'a table'`,
		`COMMENT ON COLUMN t.s IS 'a column'`,
		`COMMENT ON TYPE st IS 'a type'`,
		`COMMENT ON SCHEMA public IS 'a schema'`,
		`COMMENT ON FUNCTION f() IS 'a function'`,
	}

	forward := probeSnapshot(t, append(append(append([]string{}, first...), second...), comments...)...)
	reversed := probeSnapshot(t, append(append(append([]string{}, second...), first...), comments...)...)

	require.Equal(t, forward, reversed, "the snapshot must not depend on the order objects were created in")
}

func TestSchemaSnapshotRejectsUnsupportedObjects(t *testing.T) {
	t.Parallel()

	db := probeDB(t,
		`CREATE FUNCTION addint(int, int) RETURNS int LANGUAGE sql IMMUTABLE AS $$ SELECT $1 + $2 $$`,
		`CREATE AGGREGATE total (int) (SFUNC = addint, STYPE = int, INITCOND = '0')`,
	)

	_, err := postgres.SchemaSnapshot(t.Context(), db, "public")
	require.ErrorIs(t, err, postgres.ErrUnsupportedSchemaObject)
	require.Contains(t, err.Error(), "aggregate")
}

func TestSchemaSnapshotExcludesMigrationBookkeeping(t *testing.T) {
	t.Parallel()

	bare := probeSnapshot(t, `CREATE TABLE t (a int)`)
	withBookkeeping := probeSnapshot(t,
		`CREATE TABLE t (a int)`,
		`CREATE TABLE bun_migrations (id bigserial PRIMARY KEY, name varchar(500), group_id bigint)`,
		`CREATE TABLE bun_migration_locks (id bigserial PRIMARY KEY, table_name varchar(500))`,
	)

	require.Equal(t, bare, withBookkeeping)
}

func TestSchemaSnapshotKeepsSimilarlyNamedUserObjects(t *testing.T) {
	t.Parallel()

	snapshot := probeSnapshot(t,
		`CREATE TABLE bun_migrations_archive (id int PRIMARY KEY)`,
	)

	require.Contains(t, snapshot, "relation\tbun_migrations_archive\t")
}

func TestSchemaSnapshotEscapesRecordSeparators(t *testing.T) {
	t.Parallel()

	snapshot := probeSnapshot(t,
		`CREATE TABLE t (a int)`,
		`COMMENT ON TABLE t IS E'first\nsecond\tfield\\tail'`,
	)

	require.Contains(t, snapshot, `first\nsecond\tfield\\tail`)
	require.NotContains(t, snapshot, "first\nsecond")

	for line := range strings.SplitSeq(strings.TrimSuffix(snapshot, "\n"), "\n") {
		require.Equalf(t, 2, strings.Count(line, "\t"),
			"every snapshot record must have exactly three tab-separated fields: %q", line)
	}
}

func TestSnapshotDelta(t *testing.T) {
	t.Parallel()

	t.Run("identical snapshots have no delta", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, postgres.SnapshotDelta("a\nb\n", "b\na\n"))
	})

	t.Run("reports both directions, sorted", func(t *testing.T) {
		t.Parallel()

		require.Equal(t,
			[]string{"missing: gone", "unexpected: added"},
			postgres.SnapshotDelta("kept\ngone\n", "kept\nadded\n"))
	})

	t.Run("an empty snapshot against a populated one", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, []string{"unexpected: only"}, postgres.SnapshotDelta("", "only\n"))
	})
}
