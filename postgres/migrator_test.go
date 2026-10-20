package postgres_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/a-novel-kit/golib/postgres"
	postgrespresets "github.com/a-novel-kit/golib/postgres/presets"
)

// migrationFiles builds a migration set from file names to their SQL, each with an empty down step.
func migrationFiles(ups map[string]string) fstest.MapFS {
	files := fstest.MapFS{}

	for name, sql := range ups {
		files[name+".up.sql"] = &fstest.MapFile{Data: []byte(sql)}
		files[name+".down.sql"] = &fstest.MapFile{Data: []byte("SELECT 1;")}
	}

	return files
}

// migrationDB opens a pool on a fresh schema, with the lock_timeout a migrations job sets.
func migrationDB(t *testing.T, lockTimeout time.Duration) *bun.DB {
	t.Helper()

	admin, err := testConfig(t).DB(t.Context())
	require.NoError(t, err)

	schema := "mig_" + strings.ToLower(rand.Text())[:12]

	_, err = admin.NewRaw("CREATE SCHEMA " + schema).Exec(t.Context())
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = admin.NewRaw("DROP SCHEMA " + schema + " CASCADE").Exec(context.Background())
	})

	db, err := postgrespresets.NewDefault(
		pgdriver.WithDSN(os.Getenv("POSTGRES_DSN")),
		pgdriver.WithConnParams(map[string]any{
			"search_path":  schema,
			"lock_timeout": fmt.Sprintf("%dms", lockTimeout.Milliseconds()),
		}),
	).DB(t.Context())
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	return db
}

// appliedMigrations returns the versions bun recorded as applied in db, in order.
func appliedMigrations(t *testing.T, db *bun.DB) []string {
	t.Helper()

	var names []string

	err := db.NewRaw("SELECT name FROM bun_migrations ORDER BY name").Scan(t.Context(), &names)
	require.NoError(t, err)

	return names
}

func TestRunMigrations(t *testing.T) {
	t.Parallel()

	createProbe := map[string]string{"20260101000000_probe": "CREATE TABLE probe (id int);"}
	createSlowProbe := map[string]string{"20260101000000_slow": "CREATE TABLE probe (id int);\nSELECT pg_sleep(0.3);"}

	type run struct {
		migrations map[string]string
		expectErr  bool
		// expectApplied lists the migrations recorded once the run ends, by the version bun records.
		expectApplied []string
	}

	testCases := []struct {
		name string

		runs []run
		// concurrent starts every run at once, on the same schema.
		concurrent bool
		// blockFor holds a lock on probe from before the last run starts, for that long.
		blockFor time.Duration
	}{
		{
			name: "Success",

			runs: []run{
				{migrations: createProbe, expectApplied: []string{"20260101000000"}},
				{migrations: createProbe, expectApplied: []string{"20260101000000"}},
			},
		},
		{
			// A failed migration leaves neither its changes nor its record, so the next run applies it.
			name: "Success/FailedMigrationRunsAgain",

			runs: []run{
				{
					migrations: map[string]string{
						"20260101000000_probe":  "CREATE TABLE probe (id int);",
						"20260102000000_second": "CREATE TABLE second (id int);\nSELECT 1 / 0;",
					},
					expectErr:     true,
					expectApplied: []string{"20260101000000"},
				},
				{
					migrations: map[string]string{
						"20260101000000_probe":  "CREATE TABLE probe (id int);",
						"20260102000000_second": "CREATE TABLE second (id int);",
					},
					expectApplied: []string{"20260101000000", "20260102000000"},
				},
			},
		},
		{
			// The first attempt gives up after the lock timeout; a later one finds the lock released.
			name: "Success/LockTimeoutRetried",

			runs: []run{
				{migrations: createProbe, expectApplied: []string{"20260101000000"}},
				{
					migrations: map[string]string{
						"20260101000000_probe": "CREATE TABLE probe (id int);",
						"20260102000000_alter": "ALTER TABLE probe ADD COLUMN name text;",
					},
					expectApplied: []string{"20260101000000", "20260102000000"},
				},
			},
			blockFor: time.Second,
		},
		{
			// Runners wait for each other: the slow migration is applied once, and every runner succeeds.
			name: "Success/ConcurrentRunners",

			runs: []run{
				{migrations: createSlowProbe, expectApplied: []string{"20260101000000"}},
				{migrations: createSlowProbe, expectApplied: []string{"20260101000000"}},
				{migrations: createSlowProbe, expectApplied: []string{"20260101000000"}},
			},
			concurrent: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			db := migrationDB(t, 500*time.Millisecond)

			apply := func(run run) error {
				return postgres.RunMigrations(t.Context(), db, migrationFiles(run.migrations))
			}

			if testCase.concurrent {
				var runners sync.WaitGroup

				errs := make([]error, len(testCase.runs))

				for index, run := range testCase.runs {
					runners.Go(func() { errs[index] = apply(run) })
				}

				runners.Wait()

				for _, err := range errs {
					require.NoError(t, err)
				}

				require.Equal(t, testCase.runs[0].expectApplied, appliedMigrations(t, db))

				return
			}

			for index, run := range testCase.runs {
				if testCase.blockFor > 0 && index == len(testCase.runs)-1 {
					blocker, err := db.BeginTx(t.Context(), nil)
					require.NoError(t, err)

					_, err = blocker.NewRaw("LOCK TABLE probe IN ACCESS SHARE MODE").Exec(t.Context())
					require.NoError(t, err)

					time.AfterFunc(testCase.blockFor, func() { _ = blocker.Rollback() })
				}

				err := apply(run)
				if run.expectErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}

				require.Equal(t, run.expectApplied, appliedMigrations(t, db))
			}
		})
	}
}
