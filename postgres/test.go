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

type TransactionalTestFunc func(context.Context, *testing.T)

func NewContextTest(ctx context.Context, config Config) (context.Context, error) {
	schemaName := "ta_" + strings.ToLower(rand.Text())
	schemaName = fmt.Sprintf("%.*s", NameLen, schemaName)

	db, err := config.DBSchema(ctx, schemaName, true)
	if err != nil {
		return nil, fmt.Errorf("get db from config: %w", err)
	}

	return context.WithValue(ctx, ContextKey{}, db), nil
}

// RunIsolatedTransactionalTest runs test in a temporary throwaway schema. This allows for operations that cannot
// be performed concurrently in a transactional context, such as refreshing materialized views.
//
// This method uses a separate schema, rather than a new database, so existing extensions are still available. It
// still requires to rerun the whole migration process, so unless needed, RunTransactionalTest should be preferred.
func RunIsolatedTransactionalTest(t *testing.T, config Config, migrations fs.FS, callback TransactionalTestFunc) {
	t.Helper()

	ctx, err := NewContextTest(t.Context(), config)
	require.NoError(t, err)

	require.NoError(t, RunMigrationsContext(ctx, migrations))

	db, err := GetContext(ctx)
	require.NoError(t, err)

	ctx = context.WithValue(ctx, ContextKey{}, db)
	callback(ctx, t)
}

// RunTransactionalTest creates a special transactional context for testing. This context uses the PassthroughTx
// implementation, that allows for concurrent tests with the same database connection. It discards sub-transactions
// to prevent deadlocks.
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
	// migration set (see dbTestTemplateName), so changing a migration yields a
	// brand-new template name automatically and a stale template from older
	// code can never serve an outdated schema to a newer test binary.
	dbTestTemplatePrefix = "gotpl_"

	// dbTestInstancePrefix names a throwaway per-test database. Exactly one is
	// created per RunDBTest call and dropped in t.Cleanup.
	dbTestInstancePrefix = "gotest_"

	// dbTestMaintenanceDatabase is the database the maintenance connection
	// targets to issue CREATE DATABASE / DROP DATABASE. Neither statement can
	// run while connected to the database being created, nor (for CREATE …
	// TEMPLATE) while any session is connected to the template source, so the
	// maintenance pool deliberately points at an unrelated database.
	dbTestMaintenanceDatabase = "postgres"

	// dbTestCreateAttempts bounds the retry of CREATE DATABASE … TEMPLATE.
	// Cloning a template only fails transiently — a previous clone's
	// connections taking a moment to drain shows up as SQLSTATE 55006
	// (object_in_use); a short bounded retry absorbs that without masking a
	// real configuration error, which fails every attempt identically.
	dbTestCreateAttempts = 5
	// dbTestCreateBackoff is the wait between CREATE DATABASE … TEMPLATE
	// attempts.
	dbTestCreateBackoff = 200 * time.Millisecond
)

// dbTestOptionsConfig is the capability RunDBTest needs on top of the Config
// interface: read access to the raw driver options, so it can derive sibling
// connectors that target a different database (the maintenance database, the
// template, and the per-test clone) than the one the preset was built for.
// postgrespresets.Default satisfies this; a Config that does not is rejected
// with a clear failure rather than a panic deep in the driver.
type dbTestOptionsConfig interface {
	Options() []pgdriver.Option
}

// dbTestTemplated guards one-time template creation within a process. The first
// RunDBTest call for a given (migrations, target-cluster) pair runs the
// migrations (every other call blocks on dbTestMu meanwhile); subsequent calls
// find it flagged and return immediately, so only the first test pays the
// migration cost and the rest clone in parallel. The key includes the resolved
// connection identity (see dbTestConnKey), not just the migration hash, so the
// same migrations applied against two different clusters/roles in one process
// do not alias onto a template that exists only in the first cluster.
//
// Cross-process safety (several `go test` package binaries sharing one
// Postgres) is handled separately by the Postgres advisory lock in
// dbTestCreateTemplate; this map only deduplicates work inside a single binary.
// A failed creation is intentionally not recorded, so a transient failure is
// retried by the next test rather than poisoning the whole package run.
var (
	dbTestMu        sync.Mutex
	dbTestTemplated = map[string]bool{}

	// dbTestCloneMu serialises CREATE DATABASE … TEMPLATE within a process.
	// PostgreSQL serialises CREATE DATABASE globally on the server anyway, so
	// firing N parallel clones only produces N contending statements and a
	// retry storm; taking them one at a time client-side matches what the
	// server does regardless and removes the contention entirely. The bounded
	// retry in dbTestCreateInstance then only has to absorb *cross-process*
	// contention, not in-process self-contention.
	dbTestCloneMu sync.Mutex
)

// RunDBTest runs callback against its own freshly created PostgreSQL database,
// cloned from a migrated template via `CREATE DATABASE … TEMPLATE`. Unlike
// RunTransactionalTest (shared schema, rolled-back transaction — correct only
// when tests run serially) every RunDBTest call is physically isolated, so the
// caller may freely mark the test and its sub-tests t.Parallel() even when they
// reuse the same fixture keys.
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

	// Register the drop the instant the database exists — before the
	// maintenance pool is closed — so a Close error (which fails the test via
	// the require below) cannot strand the per-test database. It runs last
	// (t.Cleanup is LIFO): the per-test pool opened further down is closed
	// first, then the database is dropped.
	t.Cleanup(func() {
		// t.Context() is already cancelled by the time cleanups run, so use a
		// fresh background context for teardown.
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
// given migration set, creating and migrating it on first use. It serialises
// in-process callers on dbTestMu so the migrations run exactly once per
// process; cross-process duplication is prevented inside dbTestCreateTemplate.
func dbTestEnsureTemplate(ctx context.Context, options []pgdriver.Option, migrations fs.FS) (string, error) {
	name, err := dbTestTemplateName(migrations)
	if err != nil {
		return "", err
	}

	// Key by template name *and* resolved connection identity: the same
	// migrations against a different cluster/role is a different template that
	// this process has not necessarily created yet.
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

// dbTestConnKey returns a stable identifier for the Postgres target that
// options resolve to (address, user, base database). It keys the in-process
// template cache so the same migrations applied against two different clusters
// in one process are not aliased onto a single cache entry.
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
// Crucially, the migration pool is fully closed before returning: a database
// can only serve as a CREATE … TEMPLATE source while no session is connected to
// it, so any lingering connection here would break every clone.
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

	// A matching name means a previous run (this binary or another) already
	// migrated it; the content hash in the name guarantees the schema matches
	// the current migration set, so it is safe to reuse as-is.
	if exists {
		return nil
	}

	_, err = maintenance.NewRaw("CREATE DATABASE " + dbTestQuoteIdent(name)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("create template database: %w", err)
	}

	// The template database now exists but is empty/unmigrated. Any failure
	// from here must drop it: otherwise the existence probe in a later call
	// (this binary or another) would adopt the empty/partial database as a
	// valid template and run every test against a broken schema.
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

	// Must succeed: an open connection to the template makes every subsequent
	// CREATE … TEMPLATE fail with "source database is being accessed". A
	// half-open template is unusable as a clone source, so drop it too.
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

	// Serialise clones in-process: PostgreSQL serialises CREATE DATABASE
	// globally anyway, so taking them one at a time here matches the server
	// and leaves the retry below to handle only cross-process contention.
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
// preset's options by overriding only the database name. The override is a
// trailing pgdriver.WithDatabase: pgdriver applies options in order, so it wins
// over whatever database the preset's own options (a DSN, discrete options, …)
// selected, without this code having to parse them.
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
		// Close immediately rather than via defer: defer here would hold every
		// migration file open until WalkDir finished, risking fd exhaustion on
		// large OS-backed migration sets.
		_ = file.Close()

		if err != nil {
			return err
		}

		// Frame each entry unambiguously: a length-prefixed path followed by a
		// fixed-size (32-byte) content digest. Writing path and content
		// back-to-back without framing would let a path/content boundary move
		// (e.g. file "ab"/content "c" vs file "a"/content "bc") produce an
		// identical byte stream for distinct migration sets, defeating the
		// "a migration change always yields a fresh template" guarantee.
		// fs.WalkDir visits entries in lexical order, so the digest is stable
		// across runs without an explicit sort.
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
// pg_advisory_lock, so concurrent test binaries serialise template creation on
// the same value.
func dbTestAdvisoryKey(name string) int64 {
	sum := sha256.Sum256([]byte(name))

	return int64(binary.BigEndian.Uint64(sum[:8])) //nolint:gosec // wrap-around is fine for a lock key.
}

// dbTestQuoteIdent double-quotes a SQL identifier. Database names cannot be
// passed as query parameters, so they are interpolated; the generated names use
// a restricted character set, but quoting is kept as defence in depth and to
// document that these are identifiers, not literals.
func dbTestQuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
