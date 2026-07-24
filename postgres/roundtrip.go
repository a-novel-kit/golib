package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
)

const (
	roundtripInstancePrefix = "goroundtrip_"

	roundtripSnapshotExt = ".txt"

	roundtripUpdateEnv = "GOLIB_UPDATE_SNAPSHOTS"

	roundtripSchema = "public"
)

var errMigrationDrift = errors.New("schema drift")

// RoundtripOptions configures [RunMigrationRoundtripTest]. Its zero value is valid.
type RoundtripOptions struct {
	// Fixtures holds optional `<timestamp>_<name>.sql` files, applied after the named migration.
	Fixtures fs.FS

	// Snapshots holds one committed snapshot per migration. GOLIB_UPDATE_SNAPSHOTS rewrites them.
	Snapshots string
}

// RunMigrationRoundtripTest proves that every down migration exactly reverses its up.
//
// It snapshots each up, verifies each down against the preceding snapshot, then re-applies the full
// set. Fixtures run between steps; snapshots optionally enforce migration immutability.
//
// config must expose Options() []pgdriver.Option, as postgrespresets.Default does.
func RunMigrationRoundtripTest(t *testing.T, config Config, migrations fs.FS, opts *RoundtripOptions) {
	t.Helper()

	require.NoError(t, verifyRoundtrip(t.Context(), roundtripDatabase(t, config), migrations, opts))
}

func verifyRoundtrip(ctx context.Context, db *bun.DB, migrations fs.FS, opts *RoundtripOptions) error {
	if opts == nil {
		opts = &RoundtripOptions{}
	}

	discovery := migrate.NewMigrations()

	err := discovery.Discover(migrations)
	if err != nil {
		return fmt.Errorf("discover migrations: %w", err)
	}

	ordered := discovery.Sorted()
	if len(ordered) == 0 {
		return errors.New("no migrations discovered — the roundtrip would assert nothing")
	}

	err = migrate.NewMigrator(db, roundtripPrefix(ordered, len(ordered))).Init(ctx)
	if err != nil {
		return fmt.Errorf("initialise the migration bookkeeping: %w", err)
	}

	// states[k] is the schema after the first k migrations; states[0] is the virgin database.
	states := make([]string, len(ordered)+1)

	states[0], err = SchemaSnapshot(ctx, db, roundtripSchema)
	if err != nil {
		return err
	}

	for step := 1; step <= len(ordered); step++ {
		states[step], err = roundtripUp(ctx, db, ordered, step)
		if err != nil {
			return err
		}

		err = roundtripCheckSnapshot(opts, ordered[step-1], states[step])
		if err != nil {
			return err
		}

		err = roundtripApplyFixture(ctx, db, opts, ordered[step-1])
		if err != nil {
			return err
		}
	}

	for step := len(ordered); step >= 1; step-- {
		err = roundtripDown(ctx, db, ordered, step, states[step-1])
		if err != nil {
			// Later rollback errors would only repeat this first divergence.
			return err
		}
	}

	return roundtripReplay(ctx, db, ordered, states[len(ordered)])
}

func roundtripUp(ctx context.Context, db *bun.DB, ordered migrate.MigrationSlice, step int) (string, error) {
	slug := roundtripSlug(ordered[step-1])

	group, err := migrate.NewMigrator(db, roundtripPrefix(ordered, step)).Migrate(ctx)
	if err != nil {
		return "", fmt.Errorf("up migration %s: %w", slug, err)
	}

	if len(group.Migrations) != 1 {
		return "", fmt.Errorf(
			"up migration %s formed a group of %d migrations, want exactly 1 so that rolling the "+
				"group back reverts one migration", slug, len(group.Migrations))
	}

	return SchemaSnapshot(ctx, db, roundtripSchema)
}

func roundtripDown(
	ctx context.Context, db *bun.DB, ordered migrate.MigrationSlice, step int, want string,
) error {
	slug := roundtripSlug(ordered[step-1])

	group, err := migrate.NewMigrator(db, roundtripPrefix(ordered, step)).Rollback(ctx)
	if err != nil {
		return fmt.Errorf("down migration %s: %w", slug, err)
	}

	if len(group.Migrations) != 1 {
		return fmt.Errorf("the rollback of %s reverted %d migrations, want exactly 1",
			slug, len(group.Migrations))
	}

	got, err := SchemaSnapshot(ctx, db, roundtripSchema)
	if err != nil {
		return err
	}

	if delta := SnapshotDelta(want, got); len(delta) > 0 {
		return fmt.Errorf("%w: down migration %s did not restore the previous schema:\n  %s",
			errMigrationDrift, slug, strings.Join(delta, "\n  "))
	}

	return nil
}

func roundtripReplay(ctx context.Context, db *bun.DB, ordered migrate.MigrationSlice, want string) error {
	_, err := migrate.NewMigrator(db, roundtripPrefix(ordered, len(ordered))).Migrate(ctx)
	if err != nil {
		return fmt.Errorf("re-apply the migrations after a full rollback: %w", err)
	}

	got, err := SchemaSnapshot(ctx, db, roundtripSchema)
	if err != nil {
		return err
	}

	if delta := SnapshotDelta(want, got); len(delta) > 0 {
		return fmt.Errorf(
			"%w: re-applying every migration after a full rollback produced a different schema:\n  %s",
			errMigrationDrift, strings.Join(delta, "\n  "))
	}

	return nil
}

// Bun stamps every migration applied by one Migrate call with a single group id, and rolls back a
// whole group. Limiting the set to n leaves one pending migration per call.
func roundtripPrefix(ordered migrate.MigrationSlice, n int) *migrate.Migrations {
	set := migrate.NewMigrations()
	for _, migration := range ordered[:n] {
		set.Add(migration)
	}

	return set
}

// roundtripSlug rebuilds the filename stem Bun split during discovery.
func roundtripSlug(migration migrate.Migration) string {
	if migration.Comment == "" {
		return migration.Name
	}

	return migration.Name + "_" + migration.Comment
}

func roundtripApplyFixture(
	ctx context.Context, db *bun.DB, opts *RoundtripOptions, migration migrate.Migration,
) error {
	if opts.Fixtures == nil {
		return nil
	}

	name := roundtripSlug(migration) + ".sql"

	body, err := fs.ReadFile(opts.Fixtures, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("read fixture %s: %w", name, err)
	}

	_, err = db.NewRaw(string(body)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("apply fixture %s: %w", name, err)
	}

	return nil
}

func roundtripCheckSnapshot(opts *RoundtripOptions, migration migrate.Migration, got string) error {
	if opts.Snapshots == "" {
		return nil
	}

	path := filepath.Join(opts.Snapshots, roundtripSlug(migration)+roundtripSnapshotExt)

	if os.Getenv(roundtripUpdateEnv) != "" {
		err := os.MkdirAll(opts.Snapshots, 0o750)
		if err != nil {
			return fmt.Errorf("create the snapshot directory: %w", err)
		}

		err = os.WriteFile(path, []byte(got), 0o600)
		if err != nil {
			return fmt.Errorf("write snapshot %s: %w", path, err)
		}

		return nil
	}

	want, err := os.ReadFile(path) //nolint:gosec // the path is built from a migration filename.
	if err != nil {
		return fmt.Errorf("read snapshot %s — run with %s=1 to record it: %w",
			path, roundtripUpdateEnv, err)
	}

	if delta := SnapshotDelta(string(want), got); len(delta) > 0 {
		return fmt.Errorf(
			"%w: migration %s no longer produces its committed schema. A merged migration must not "+
				"change; if this one legitimately did, re-record with %s=1.\n  %s",
			errMigrationDrift, roundtripSlug(migration), roundtripUpdateEnv, strings.Join(delta, "\n  "))
	}

	return nil
}

func roundtripDatabase(t *testing.T, config Config) *bun.DB {
	t.Helper()

	optionsConfig, ok := config.(dbTestOptionsConfig)
	require.Truef(t, ok,
		"RunMigrationRoundtripTest requires a Config exposing Options() []pgdriver.Option "+
			"(e.g. postgrespresets.Default)")

	db, err := newRoundtripDatabase(t, optionsConfig.Options())
	require.NoError(t, err)

	return db
}

func newRoundtripDatabase(t *testing.T, options []pgdriver.Option) (*bun.DB, error) {
	t.Helper()

	instance := roundtripInstancePrefix + strings.ToLower(rand.Text())

	maintenance, err := dbTestOpen(t.Context(), options, dbTestMaintenanceDatabase)
	if err != nil {
		return nil, err
	}

	_, err = maintenance.NewRaw("CREATE DATABASE " + dbTestQuoteIdent(instance)).Exec(t.Context())
	if err != nil {
		_ = maintenance.Close()

		return nil, fmt.Errorf("create the roundtrip database: %w", err)
	}

	// Register before opening the test pool so cleanup runs after that pool closes.
	t.Cleanup(func() {
		// t.Context is already cancelled by the time cleanups run.
		ctx := context.Background()

		cleanup, cleanupErr := dbTestOpen(ctx, options, dbTestMaintenanceDatabase)
		if cleanupErr != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()

		_ = dbTestDropDatabase(ctx, cleanup, instance)
	})

	err = maintenance.Close()
	if err != nil {
		return nil, err
	}

	db, err := dbTestOpen(t.Context(), options, instance)
	if err != nil {
		return nil, err
	}

	t.Cleanup(func() { _ = db.Close() })

	return db, nil
}
