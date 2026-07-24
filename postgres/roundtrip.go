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
	// roundtripInstancePrefix names the throwaway database one roundtrip runs against. Unlike the
	// databases RunDBTest clones, it starts empty: the run's whole subject is what the migrations
	// do to a virgin database, so there is nothing to clone from.
	roundtripInstancePrefix = "goroundtrip_"

	// roundtripSnapshotExt is the extension of a committed snapshot. Plain text, because its
	// review value is that a reader can see a migration's effect on the schema in a pull request.
	roundtripSnapshotExt = ".txt"

	// roundtripUpdateEnv, when set to any non-empty value, makes a run rewrite the committed
	// snapshots instead of comparing against them.
	//
	// An environment variable rather than a test flag: this package is linked into every
	// consumer's test binary, and a flag registered here would collide with a consumer that
	// defines its own.
	roundtripUpdateEnv = "GOLIB_UPDATE_SNAPSHOTS"

	// roundtripSchema is the schema migrations land in. Services run one schema per database, so
	// the roundtrip has no reason to take it as a parameter.
	roundtripSchema = "public"
)

// errMigrationDrift reports that the schema is not where a migration should have left it. It stays
// unexported because RunMigrationRoundtripTest fails the test rather than handing it back; only
// this package's own tests distinguish it from an infrastructure failure.
var errMigrationDrift = errors.New("schema drift")

// RoundtripOptions tunes [RunMigrationRoundtripTest]. The zero value runs the roundtrip against a
// schema that never holds rows and keeps no snapshots on disk.
type RoundtripOptions struct {
	// Fixtures holds SQL files named for the migration they follow, as `<timestamp>_<name>.sql`.
	// A fixture runs after its migration, so its rows are what the next migration's up and its own
	// migration's down both have to survive.
	//
	// Seeding at the step rather than at the end is what puts data in front of the up migrations
	// too: a column added NOT NULL with no default succeeds on an empty table and fails on a
	// populated one, and only the second is what production does.
	//
	// A migration with no matching file simply seeds nothing.
	Fixtures fs.FS

	// Snapshots is the directory holding one committed snapshot per migration. When set, each
	// snapshot taken on the way up is compared against the file for that migration, so an edit to
	// an already-merged migration surfaces as a diff on a file that should never move again.
	//
	// Setting GOLIB_UPDATE_SNAPSHOTS rewrites the files instead of comparing.
	Snapshots string
}

// RunMigrationRoundtripTest proves that every down migration exactly reverses its up.
//
// It applies the migrations one at a time against a throwaway database, snapshotting the schema
// after each, then rolls them back one at a time and requires every rollback to land exactly on the
// state before its migration. Finally it re-applies the whole set, which catches a down migration
// that leaves the database in a state the up cannot run against a second time.
//
// Verifying each step, rather than only the end state, is what localises a failure to the migration
// that caused it. A rollback that overshoots and one that stops short both end at the wrong place,
// but the step that first diverged names which migration to fix.
//
// config must expose Options() []pgdriver.Option, as postgrespresets.Default does.
func RunMigrationRoundtripTest(t *testing.T, config Config, migrations fs.FS, opts *RoundtripOptions) {
	t.Helper()

	require.NoError(t, verifyRoundtrip(t.Context(), roundtripDatabase(t, config), migrations, opts))
}

// verifyRoundtrip carries the whole roundtrip. It returns errors rather than failing a test so that
// this package can test what it does when a migration set is broken, which is the behaviour that
// matters most.
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
			// Drift cascades: every remaining rollback would report the same difference, so the
			// run stops at the migration that introduced it rather than repeating itself.
			return err
		}
	}

	return roundtripReplay(ctx, db, ordered, states[len(ordered)])
}

// roundtripUp applies migration number step and nothing else, and snapshots the result.
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

// roundtripDown rolls back migration number step and requires the schema to land on want.
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

// roundtripReplay re-applies every migration to the rolled-back database and requires the schema to
// come back identical.
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

// roundtripPrefix builds a migration set holding only the first n of ordered.
//
// Bun stamps every migration applied by one Migrate call with a single group id, and rolls back a
// whole group at a time. A set that ends at migration n therefore leaves exactly one migration
// pending, which is what makes its group one migration wide — and a rollback of that group revert
// one migration rather than the entire set.
func roundtripPrefix(ordered migrate.MigrationSlice, n int) *migrate.Migrations {
	set := migrate.NewMigrations()
	for _, migration := range ordered[:n] {
		set.Add(migration)
	}

	return set
}

// roundtripSlug rebuilds the stem of a migration's filename, which is how its fixture and its
// committed snapshot are named. Bun splits the filename into a timestamp and a comment; neither
// half identifies a migration on its own.
func roundtripSlug(migration migrate.Migration) string {
	if migration.Comment == "" {
		return migration.Name
	}

	return migration.Name + "_" + migration.Comment
}

// roundtripApplyFixture runs the fixture belonging to a migration, if there is one.
func roundtripApplyFixture(
	ctx context.Context, db *bun.DB, opts *RoundtripOptions, migration migrate.Migration,
) error {
	if opts.Fixtures == nil {
		return nil
	}

	name := roundtripSlug(migration) + ".sql"

	body, err := fs.ReadFile(opts.Fixtures, name)
	if err != nil {
		// A migration that needs no rows of its own is the common case, so an absent fixture is
		// not a failure. Any other read error is.
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

// roundtripCheckSnapshot compares a snapshot against its committed file, or rewrites the file when
// the update variable is set.
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

// roundtripDatabase creates an empty database for one roundtrip and drops it on cleanup.
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

// newRoundtripDatabase creates the throwaway database and registers its teardown.
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

	// Registered while the maintenance pool is still open, so a failure below cannot strand the
	// database. Cleanup is LIFO, so the pool opened further down closes before the drop runs.
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
