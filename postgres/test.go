package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

// TransactionalTestFunc is the body of a database-backed test, run with a
// context carrying the connection isolated for that test.
type TransactionalTestFunc func(context.Context, *testing.T)

// schemaDropper is the optional capability a Config offers to remove a schema created
// through DBSchema. The bundled Default implements it; a Config that does not simply
// leaves its throwaway schemas in place, as before.
type schemaDropper interface {
	DropSchema(ctx context.Context, schema string) error
}

// NewContextTest derives a context bound to a fresh, randomly named schema created
// through config, isolating the test from others sharing the database. It returns the
// schema name so the caller can drop it once done.
func NewContextTest(ctx context.Context, config Config) (context.Context, string, error) {
	schemaName := "ta_" + strings.ToLower(rand.Text())
	schemaName = fmt.Sprintf("%.*s", NameLen, schemaName)

	db, err := config.DBSchema(ctx, schemaName, true)
	if err != nil {
		return nil, "", fmt.Errorf("get db from config: %w", err)
	}

	return context.WithValue(ctx, ContextKey{}, db), schemaName, nil
}

// RunIsolatedTransactionalTest runs callback in a throwaway schema, which admits
// operations a transaction cannot host concurrently, such as refreshing a
// materialized view.
//
// The schema lives in the existing database, so its extensions remain available.
// Each call reruns the whole migration set, which makes RunTransactionalTest the
// cheaper default.
func RunIsolatedTransactionalTest(t *testing.T, config Config, migrations fs.FS, callback TransactionalTestFunc) {
	t.Helper()

	ctx, schema, err := NewContextTest(t.Context(), config)
	require.NoError(t, err)

	if dropper, ok := config.(schemaDropper); ok {
		t.Cleanup(func() {
			// t.Context is cancelled by the time cleanup runs, so the drop gets a fresh
			// context. A failure here leaks the schema but must not fail the test.
			dropErr := dropper.DropSchema(context.WithoutCancel(ctx), schema)
			if dropErr != nil {
				t.Logf("drop test schema %s: %v", schema, dropErr)
			}
		})
	}

	require.NoError(t, RunMigrationsContext(ctx, migrations))

	db, err := GetContext(ctx)
	require.NoError(t, err)

	ctx = context.WithValue(ctx, ContextKey{}, db)
	callback(ctx, t)
}

// RunTransactionalTest runs callback inside a transaction that is rolled back on
// cleanup. The context carries a PassthroughTx, which discards sub-transactions
// so concurrent calls sharing the connection cannot deadlock.
func RunTransactionalTest(t *testing.T, config Config, callback TransactionalTestFunc) {
	t.Helper()

	ctx, err := NewContext(t.Context(), config)
	require.NoError(t, err)

	db, err := GetContext(ctx)
	require.NoError(t, err)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = tx.Rollback()
	})

	ctx = context.WithValue(ctx, ContextKey{}, NewPassthroughTx(tx))
	callback(ctx, t)
}

const (
	// dbTestTemplatePrefix names the migrated template database that every
	// per-test database is cloned from. Its suffix is a content hash of the
	// migration set (see dbTestTemplateName), so a migration change yields a new
	// template name and no test binary can be served an outdated schema.
	dbTestTemplatePrefix = "gotpl_"

	// dbTestInstancePrefix names a throwaway per-test database. Exactly one is
	// created per RunDBTest call and dropped in t.Cleanup.
	dbTestInstancePrefix = "gotest_"

	// dbTestMaintenanceDatabase is the database the maintenance connection
	// targets to issue CREATE DATABASE / DROP DATABASE. Neither statement can run
	// from the database being created, and CREATE … TEMPLATE needs the template
	// source free of sessions, so the maintenance pool points at an unrelated
	// database.
	dbTestMaintenanceDatabase = "postgres"

	// dbTestCreateAttempts bounds the retry of CREATE DATABASE … TEMPLATE. A
	// previous clone's connections taking a moment to drain surfaces as SQLSTATE
	// 55006 (object_in_use), which a short bounded retry absorbs; a real
	// configuration error fails every attempt alike.
	dbTestCreateAttempts = 5
	// dbTestCreateBackoff is the wait between CREATE DATABASE … TEMPLATE
	// attempts.
	dbTestCreateBackoff = 200 * time.Millisecond
)

// dbTestOptionsConfig is the capability RunDBTest needs on top of the Config
// interface: read access to the raw driver options, so it can derive sibling
// connectors targeting another database in the same cluster, such as the
// per-test clone. postgrespresets.Default satisfies it.
type dbTestOptionsConfig interface {
	Options() []pgdriver.Option
}

// dbTestTemplated guards one-time template creation within a process. The first
// RunDBTest call for a given (migrations, target-cluster) pair runs the
// migrations while every other call blocks on dbTestMu, so only the first test
// pays the migration cost and the rest clone in parallel. The key includes the
// resolved connection identity (see dbTestConnKey), so the same migrations
// applied against two clusters in one process do not alias onto a template that
// exists in only one of them.
//
// Cross-process safety comes from the Postgres advisory lock in
// dbTestCreateTemplate; this map deduplicates work inside a single binary. A
// failed creation is not recorded, so the next test retries it.
var (
	dbTestMu        sync.Mutex
	dbTestTemplated = map[string]bool{}

	// dbTestCloneMu serializes CREATE DATABASE … TEMPLATE within a process.
	// PostgreSQL serializes CREATE DATABASE globally, so parallel clones only
	// produce contending statements and a retry storm. Taking them one at a time
	// leaves the bounded retry in dbTestCreateInstance to absorb cross-process
	// contention alone.
	dbTestCloneMu sync.Mutex
)

// RunDBTest runs callback against its own freshly created PostgreSQL database,
// cloned from a migrated template via `CREATE DATABASE … TEMPLATE`. Every call
// is physically isolated, so the caller may mark the test and its sub-tests
// t.Parallel() even when they reuse the same fixture keys.
//
// The cost model is "migrate once, clone many": the migration set is applied a
// single time into a template database whose name is a hash of the migrations,
// and each test gets a fast file-level copy of it. The per-test database is
// dropped (WITH FORCE) in t.Cleanup; the template is left in place and reused
// across runs as long as the migrations are unchanged.
//
// config must expose Options() []pgdriver.Option (postgrespresets.Default
// does). callback receives a context carrying a real *bun.DB for the per-test
// database, retrievable with GetContext.
func RunDBTest(t *testing.T, config Config, migrations fs.FS, callback TransactionalTestFunc) {
	t.Helper()

	optionsConfig, ok := config.(dbTestOptionsConfig)
	require.Truef(t, ok,
		"RunDBTest requires a Config exposing Options() []pgdriver.Option (e.g. postgrespresets.Default)")

	options := optionsConfig.Options()

	template, err := dbTestEnsureTemplate(t.Context(), options, migrations)
	require.NoError(t, err)

	instance := dbTestInstancePrefix + strings.ToLower(rand.Text())

	maintenance, err := dbTestOpen(t.Context(), options, dbTestMaintenanceDatabase)
	require.NoError(t, err)

	err = dbTestCreateInstance(t.Context(), maintenance, instance, template)
	require.NoError(t, err)

	// Register the drop the instant the database exists, before the maintenance
	// pool is closed, so a Close error (which fails the test via the require
	// below) cannot strand the per-test database. t.Cleanup is LIFO, so the
	// per-test pool opened further down is closed first, then the database is
	// dropped.
	t.Cleanup(func() {
		// t.Context() is already canceled by the time cleanups run.
		ctx := context.Background()

		cleanup, cleanupErr := dbTestOpen(ctx, options, dbTestMaintenanceDatabase)
		if cleanupErr != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()

		_ = dbTestDropDatabase(ctx, cleanup, instance)
	})

	// The maintenance pool is only needed for the CREATE; close it so it does
	// not idle for the lifetime of the test.
	require.NoError(t, maintenance.Close())

	db, err := dbTestOpen(t.Context(), options, instance)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	ctx := context.WithValue(t.Context(), ContextKey{}, db)
	callback(ctx, t)
}

// dbTestEnsureTemplate returns the name of a migrated template database for the
// given migration set, creating and migrating it on first use. It serializes
// in-process callers on dbTestMu so the migrations run exactly once per
// process; cross-process duplication is prevented inside dbTestCreateTemplate.
func dbTestEnsureTemplate(ctx context.Context, options []pgdriver.Option, migrations fs.FS) (string, error) {
	name, err := dbTestTemplateName(migrations)
	if err != nil {
		return "", err
	}

	// Key by template name and resolved connection identity: the same migrations
	// against a different cluster or role is a different template, which this
	// process has not necessarily created yet.
	cacheKey := name + "\x00" + dbTestConnKey(options)

	dbTestMu.Lock()
	defer dbTestMu.Unlock()

	if dbTestTemplated[cacheKey] {
		return name, nil
	}

	err = dbTestCreateTemplate(ctx, options, name, migrations)
	if err != nil {
		return "", err
	}

	dbTestTemplated[cacheKey] = true

	return name, nil
}

// dbTestConnKey returns a stable identifier for the Postgres target that options
// resolve to (address, user, base database). It keys the in-process template
// cache, so the same migrations applied against two clusters in one process get
// two cache entries.
func dbTestConnKey(options []pgdriver.Option) string {
	config := pgdriver.NewConnector(options...).Config()

	return config.Addr + "\x00" + config.User + "\x00" + config.Database
}

// dbTestCreateTemplate creates and migrates the template database called name,
// unless another test binary already did. It holds a Postgres session-level
// advisory lock for the duration so concurrent `go test` package binaries
// cannot race on the same template; the lock is keyed by a hash of the name and
// scoped to the maintenance connection (closing the connection releases it even
// if the explicit unlock is skipped on an error path).
//
// The migration pool is fully closed before returning. A database can only serve as a
// CREATE … TEMPLATE source while no session is connected to it.
func dbTestCreateTemplate(
	ctx context.Context, options []pgdriver.Option, name string, migrations fs.FS,
) error {
	maintenance, err := dbTestOpen(ctx, options, dbTestMaintenanceDatabase)
	if err != nil {
		return err
	}
	defer func() { _ = maintenance.Close() }()

	lockKey := dbTestAdvisoryKey(name)

	_, err = maintenance.NewRaw("SELECT pg_advisory_lock(?)", lockKey).Exec(ctx)
	if err != nil {
		return fmt.Errorf("acquire template advisory lock: %w", err)
	}
	defer func() {
		// Best-effort: the lock is also released when the maintenance
		// connection closes just below.
		_, _ = maintenance.NewRaw("SELECT pg_advisory_unlock(?)", lockKey).Exec(ctx)
	}()

	var exists bool

	err = maintenance.NewRaw(
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = ?)", name,
	).Scan(ctx, &exists)
	if err != nil {
		return fmt.Errorf("probe template database: %w", err)
	}

	// A matching name means a previous run already migrated it, and the content
	// hash in the name guarantees the schema matches the current migration set,
	// so it is safe to reuse as-is.
	if exists {
		return nil
	}

	_, err = maintenance.NewRaw("CREATE DATABASE " + dbTestQuoteIdent(name)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("create template database: %w", err)
	}

	// The template database now exists but is unmigrated, so any failure from
	// here must drop it; otherwise the existence probe in a later call adopts the
	// partial database as a valid template and every test runs against a broken
	// schema.
	templateDB, err := dbTestOpen(ctx, options, name)
	if err != nil {
		_ = dbTestDropDatabase(ctx, maintenance, name)

		return err
	}

	err = RunMigrations(ctx, templateDB, migrations)
	if err != nil {
		_ = templateDB.Close()
		_ = dbTestDropDatabase(ctx, maintenance, name)

		return fmt.Errorf("migrate template database: %w", err)
	}

	// An open connection to the template makes every subsequent CREATE … TEMPLATE
	// fail with "source database is being accessed", so a template whose
	// connection cannot be closed is dropped.
	err = templateDB.Close()
	if err != nil {
		_ = dbTestDropDatabase(ctx, maintenance, name)

		return fmt.Errorf("close template connection: %w", err)
	}

	return nil
}

// dbTestDropDatabase drops database name using the supplied maintenance
// connection. WITH (FORCE) (PostgreSQL 13+) terminates any backend still
// attached so a leaked connection cannot block the drop.
func dbTestDropDatabase(ctx context.Context, maintenance *bun.DB, name string) error {
	_, err := maintenance.NewRaw(
		"DROP DATABASE IF EXISTS " + dbTestQuoteIdent(name) + " WITH (FORCE)",
	).Exec(ctx)
	if err != nil {
		return fmt.Errorf("drop database %q: %w", name, err)
	}

	return nil
}

// dbTestCreateInstance clones template into a fresh database called instance,
// retrying briefly on the transient object_in_use failure that a still-draining
// previous clone can cause.
func dbTestCreateInstance(ctx context.Context, maintenance *bun.DB, instance, template string) error {
	query := fmt.Sprintf(
		"CREATE DATABASE %s TEMPLATE %s",
		dbTestQuoteIdent(instance), dbTestQuoteIdent(template),
	)

	dbTestCloneMu.Lock()
	defer dbTestCloneMu.Unlock()

	var err error

	for attempt := range dbTestCreateAttempts {
		_, err = maintenance.NewRaw(query).Exec(ctx)
		if err == nil {
			return nil
		}

		if attempt < dbTestCreateAttempts-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(dbTestCreateBackoff):
			}
		}
	}

	return fmt.Errorf("clone template into test database: %w", err)
}

// dbTestOpen opens a bun.DB for a single named database, derived from the
// preset's options by overriding only the database name. pgdriver applies
// options in order, so a trailing pgdriver.WithDatabase wins over whatever
// database the preset's own options selected, with no need to parse them.
func dbTestOpen(ctx context.Context, options []pgdriver.Option, database string) (*bun.DB, error) {
	options = append(append([]pgdriver.Option{}, options...), pgdriver.WithDatabase(database))

	db := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(options...)), pgdialect.New(),
		bun.WithDiscardUnknownColumns())

	err := Ping(ctx, db)
	if err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("open test database %q: %w", database, err)
	}

	return db, nil
}

// dbTestTemplateName derives the template database name from a hash of every
// file in the migration set (path and content), so any migration change
// produces a different template and never reuses a stale schema.
func dbTestTemplateName(migrations fs.FS) (string, error) {
	digest := sha256.New()

	err := fs.WalkDir(migrations, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		file, err := migrations.Open(path)
		if err != nil {
			return err
		}

		fileDigest := sha256.New()

		_, err = io.Copy(fileDigest, file)
		// Closing here keeps one migration file open at a time, however large the
		// migration set.
		_ = file.Close()

		if err != nil {
			return err
		}

		// Frame each entry as a length-prefixed path followed by a fixed-size
		// (32-byte) content digest, so a shifting path/content boundary cannot
		// give two distinct migration sets the same byte stream. fs.WalkDir
		// visits entries in lexical order, so the digest is stable across runs
		// without an explicit sort.
		var pathLen [8]byte

		binary.BigEndian.PutUint64(pathLen[:], uint64(len(path)))
		_, _ = digest.Write(pathLen[:])
		_, _ = digest.Write([]byte(path))
		_, _ = digest.Write(fileDigest.Sum(nil))

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hash migrations: %w", err)
	}

	sum := digest.Sum(nil)

	return dbTestTemplatePrefix + hex.EncodeToString(sum[:8]), nil
}

// dbTestAdvisoryKey maps a database name to the int64 key used with
// pg_advisory_lock, so concurrent test binaries serialize template creation on
// the same value.
func dbTestAdvisoryKey(name string) int64 {
	sum := sha256.Sum256([]byte(name))

	return int64(binary.BigEndian.Uint64(sum[:8])) //nolint:gosec // wrap-around is fine for a lock key.
}

// dbTestQuoteIdent double-quotes a SQL identifier. Database names cannot be
// bound as query parameters, so they are interpolated; the generated names use a
// restricted character set, and quoting adds defense in depth.
func dbTestQuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
