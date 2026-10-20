package postgrestest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"

	"github.com/a-novel-kit/golib/postgres"
)

const (
	// roundtripInstancePrefix names the throwaway database of one roundtrip.
	roundtripInstancePrefix = "goroundtrip_"

	roundtripSnapshotExt = ".txt"

	// roundtripUpdateEnv names the variable that records the snapshots instead of checking them.
	roundtripUpdateEnv = "GOLIB_UPDATE_SNAPSHOTS"

	// roundtripHistoryRecord starts the first record of a snapshot, which binds it to the history
	// digest of its migration.
	roundtripHistoryRecord = "migration-history\tsha256:"

	// roundtripSchema is the schema the roundtrip migrates and snapshots.
	roundtripSchema = "public"
)

// errMigrationDrift reports a schema that differs from the one the roundtrip expected.
var errMigrationDrift = errors.New("schema drift")

// RoundtripOptions configures [RunMigrationRoundtripTest]. Its zero value is valid.
type RoundtripOptions struct {
	// Fixtures holds optional `<timestamp>_<name>.sql` files, applied after the named migration.
	Fixtures fs.FS

	// Snapshots is the directory holding one schema snapshot per migration, each bound to the
	// migration history up to it. Empty skips them. Setting GOLIB_UPDATE_SNAPSHOTS rewrites them.
	Snapshots string
}

// RunMigrationRoundtripTest proves that every down migration exactly reverses its up.
//
// It snapshots each up, verifies each down against the preceding snapshot, then re-applies the full
// set. Fixtures run between steps. Snapshots bind each schema to its exact migration prefix.
//
// config must expose Options() []pgdriver.Option, as postgrespresets.Default does.
func RunMigrationRoundtripTest(t *testing.T, config postgres.Config, migrations fs.FS, opts *RoundtripOptions) {
	t.Helper()

	require.NoError(t, verifyRoundtrip(t.Context(), roundtripDatabase(t, config), migrations, opts))
}

// verifyRoundtrip runs the roundtrip [RunMigrationRoundtripTest] describes against db, which starts
// empty.
func verifyRoundtrip(ctx context.Context, db *bun.DB, migrations fs.FS, opts *RoundtripOptions) error {
	if opts == nil {
		opts = &RoundtripOptions{}
	}

	ordered, historyDigests, err := roundtripPlan(migrations, opts)
	if err != nil {
		return err
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
		states[step], err = roundtripUp(ctx, db, opts, ordered, step, historyDigests[step-1])
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

// roundtripPlan discovers the migrations in order, with the history digest each snapshot is bound
// to. The digests stay empty when opts records no snapshots.
func roundtripPlan(migrations fs.FS, opts *RoundtripOptions) (migrate.MigrationSlice, []string, error) {
	discovery := migrate.NewMigrations()

	err := discovery.Discover(migrations)
	if err != nil {
		return nil, nil, fmt.Errorf("discover migrations: %w", err)
	}

	ordered := discovery.Sorted()
	if len(ordered) == 0 {
		return nil, nil, errors.New("no migrations discovered — the roundtrip would assert nothing")
	}

	if opts.Snapshots == "" {
		return ordered, make([]string, len(ordered)), nil
	}

	historyDigests, err := roundtripHistoryDigests(migrations, ordered)
	if err != nil {
		return nil, nil, err
	}

	return ordered, historyDigests, nil
}

// roundtripUp applies migration step alone, checks the schema it leaves against its snapshot, then
// applies its fixture. It returns that schema.
func roundtripUp(
	ctx context.Context,
	db *bun.DB,
	opts *RoundtripOptions,
	ordered migrate.MigrationSlice,
	step int,
	historyDigest string,
) (string, error) {
	migration := ordered[step-1]
	slug := roundtripSlug(migration)

	group, err := migrate.NewMigrator(db, roundtripPrefix(ordered, step)).Migrate(ctx)
	if err != nil {
		return "", fmt.Errorf("up migration %s: %w", slug, err)
	}

	if len(group.Migrations) != 1 {
		return "", fmt.Errorf(
			"up migration %s formed a group of %d migrations, want exactly 1 so that rolling the "+
				"group back reverts one migration", slug, len(group.Migrations))
	}

	state, err := SchemaSnapshot(ctx, db, roundtripSchema)
	if err != nil {
		return "", err
	}

	err = roundtripCheckSnapshot(opts, migration, state, historyDigest)
	if err != nil {
		return "", err
	}

	err = roundtripApplyFixture(ctx, db, opts, migration)
	if err != nil {
		return "", err
	}

	return state, nil
}

// roundtripDown rolls migration step back alone, and checks the schema it leaves is want.
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

// roundtripReplay applies every migration again after the full rollback, and checks the schema it
// leaves is want.
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

// roundtripPrefix returns the first n migrations of ordered. Bun stamps every migration one Migrate
// call applies with a single group id, and rolls back a whole group, so a set limited to n leaves
// one pending migration per call.
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

// roundtripApplyFixture runs the fixture named after migration, when opts holds one.
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

// roundtripCheckSnapshot compares got, bound to historyDigest, with the snapshot recorded for
// migration. It records the snapshot instead when GOLIB_UPDATE_SNAPSHOTS is set.
func roundtripCheckSnapshot(
	opts *RoundtripOptions, migration migrate.Migration, got, historyDigest string,
) error {
	if opts.Snapshots == "" {
		return nil
	}

	snapshotPath := filepath.Join(opts.Snapshots, roundtripSlug(migration)+roundtripSnapshotExt)
	got = roundtripHistoryRecord + historyDigest + "\n" + got

	if os.Getenv(roundtripUpdateEnv) != "" {
		err := os.MkdirAll(opts.Snapshots, 0o750)
		if err != nil {
			return fmt.Errorf("create the snapshot directory: %w", err)
		}

		err = os.WriteFile(snapshotPath, []byte(got), 0o600)
		if err != nil {
			return fmt.Errorf("write snapshot %s: %w", snapshotPath, err)
		}

		return nil
	}

	want, err := os.ReadFile(snapshotPath) //nolint:gosec // Built from a migration filename.
	if err != nil {
		return fmt.Errorf("read snapshot %s — run with %s=1 to record it: %w",
			snapshotPath, roundtripUpdateEnv, err)
	}

	if delta := SnapshotDelta(string(want), got); len(delta) > 0 {
		return fmt.Errorf(
			"%w: migration %s no longer matches its committed history and schema. A merged "+
				"migration must not change; re-record a reviewed exception with %s=1.\n  %s",
			errMigrationDrift, roundtripSlug(migration), roundtripUpdateEnv, strings.Join(delta, "\n  "))
	}

	return nil
}

// roundtripMigrationSources holds the up and down source files of one migration.
type roundtripMigrationSources struct {
	up   string
	down string
}

// roundtripSourceSet maps each migration slug to its source files.
type roundtripSourceSet map[string]roundtripMigrationSources

// roundtripSourceSuffixes lists the file suffixes Bun discovers migrations under. Each
// transactional suffix precedes the plain suffix it ends with.
var roundtripSourceSuffixes = []struct {
	value  string
	isDown bool
}{
	{value: ".tx.up.sql"},
	{value: ".up.sql"},
	{value: ".tx.down.sql", isDown: true},
	{value: ".down.sql", isDown: true},
}

// roundtripHistoryDigests chains one digest over the up and down files of each migration in order,
// so the digest of a migration covers it and every migration before it.
func roundtripHistoryDigests(
	migrations fs.FS, ordered migrate.MigrationSlice,
) ([]string, error) {
	sources, err := roundtripSources(migrations)
	if err != nil {
		return nil, fmt.Errorf("discover migration history: %w", err)
	}

	digests := make([]string, len(ordered))
	history := sha256.New()

	for index, migration := range ordered {
		files, err := sources.files(roundtripSlug(migration))
		if err != nil {
			return nil, err
		}

		for _, sourcePath := range files {
			body, err := fs.ReadFile(migrations, sourcePath)
			if err != nil {
				return nil, fmt.Errorf("read migration history file %s: %w", sourcePath, err)
			}

			hashField(history, []byte(sourcePath))
			hashField(history, body)
		}

		digests[index] = hex.EncodeToString(history.Sum(nil))
	}

	return digests, nil
}

// roundtripSources pairs the up and down SQL files of each migration by slug.
func roundtripSources(migrations fs.FS) (roundtripSourceSet, error) {
	paths, err := migrationFiles(migrations)
	if err != nil {
		return nil, err
	}

	sources := roundtripSourceSet{}

	for _, sourcePath := range paths {
		slug, isDown, ok := roundtripSourceName(path.Base(sourcePath))
		if !ok {
			continue
		}

		pair := sources[slug]

		target, direction := &pair.up, "up"
		if isDown {
			target, direction = &pair.down, "down"
		}

		if *target != "" {
			return nil, fmt.Errorf("migration %s has duplicate %s files", slug, direction)
		}

		*target = sourcePath
		sources[slug] = pair
	}

	return sources, nil
}

// files returns the up file, then the down file, of the migration called slug.
func (sources roundtripSourceSet) files(slug string) ([]string, error) {
	pair := sources[slug]
	if pair.up == "" || pair.down == "" {
		return nil, fmt.Errorf("migration %s needs one up and one down SQL file", slug)
	}

	return []string{pair.up, pair.down}, nil
}

// roundtripSourceName splits a migration file name into its slug and direction. It reports false
// for a file Bun does not discover as a migration.
func roundtripSourceName(name string) (string, bool, bool) {
	for _, suffix := range roundtripSourceSuffixes {
		if slug, found := strings.CutSuffix(name, suffix.value); found {
			return slug, suffix.isDown, true
		}
	}

	return "", false, false
}

// roundtripDatabase creates the empty database a roundtrip runs in, dropped with the test.
func roundtripDatabase(t *testing.T, config postgres.Config) *bun.DB {
	t.Helper()

	optionsConfig, ok := config.(dbTestOptionsConfig)
	require.Truef(t, ok,
		"RunMigrationRoundtripTest requires a Config exposing Options() []pgdriver.Option "+
			"(e.g. postgrespresets.Default)")

	db, err := newRoundtripDatabase(t, optionsConfig.Options())
	require.NoError(t, err)

	return db
}

// newRoundtripDatabase creates an empty, randomly named database from options, dropped with the
// test.
func newRoundtripDatabase(t *testing.T, options []pgdriver.Option) (*bun.DB, error) {
	t.Helper()

	instance := roundtripInstancePrefix + strings.ToLower(rand.Text())

	return dbTestNewDatabase(t, options, instance, func(ctx context.Context, maintenance *bun.DB) error {
		return createRoundtripDatabase(ctx, maintenance, instance)
	})
}

// createRoundtripDatabase creates the empty database called instance on a maintenance connection.
func createRoundtripDatabase(ctx context.Context, maintenance *bun.DB, instance string) error {
	_, err := maintenance.NewRaw("CREATE DATABASE " + dbTestQuoteIdent(instance)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("create the roundtrip database: %w", err)
	}

	return nil
}
