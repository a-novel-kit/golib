package postgrestest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/a-novel-kit/golib/postgres"
)

// schemaNameLen caps the length of a test schema name, keeping it within PostgreSQL's 63-byte
// identifier limit.
const schemaNameLen = 31

// TransactionalTestFunc is the body of a database-backed test, run with a
// context carrying the connection isolated for that test.
type TransactionalTestFunc func(context.Context, *testing.T)

// schemaDropper is the optional capability a Config offers to remove a schema created
// through DBSchema. The bundled Default implements it. A Config without it leaves its
// throwaway schemas in place.
type schemaDropper interface {
	DropSchema(ctx context.Context, schema string) error
}

// NewContextTest derives a context bound to a fresh, randomly named schema created
// through config, isolating the test from others sharing the database. It returns the
// schema name so the caller can drop it once done.
func NewContextTest(ctx context.Context, config postgres.Config) (context.Context, string, error) {
	schemaName := "ta_" + strings.ToLower(rand.Text())
	schemaName = fmt.Sprintf("%.*s", schemaNameLen, schemaName)

	db, err := config.DBSchema(ctx, schemaName, true)
	if err != nil {
		return nil, "", fmt.Errorf("get db from config: %w", err)
	}

	return context.WithValue(ctx, postgres.ContextKey{}, db), schemaName, nil
}

// RunIsolatedTransactionalTest runs callback in a throwaway schema, which admits
// operations a transaction cannot host concurrently, such as refreshing a
// materialized view.
//
// The schema lives in the existing database, so its extensions remain available.
// Each call reruns the whole migration set, which makes RunTransactionalTest the
// cheaper default.
func RunIsolatedTransactionalTest(
	t *testing.T,
	config postgres.Config,
	migrations fs.FS,
	callback TransactionalTestFunc,
) {
	t.Helper()

	ctx, schema, err := NewContextTest(t.Context(), config)
	require.NoError(t, err)

	if dropper, ok := config.(schemaDropper); ok {
		t.Cleanup(func() { dropTestSchema(ctx, t, dropper, schema) })
	}

	require.NoError(t, postgres.RunMigrationsContext(ctx, migrations))

	callback(ctx, t)
}

// dropTestSchema drops schema from a test cleanup. A failure leaks the schema, and the test still
// passes.
func dropTestSchema(ctx context.Context, t *testing.T, dropper schemaDropper, schema string) {
	t.Helper()

	// t.Context is canceled by the time cleanups run.
	err := dropper.DropSchema(context.WithoutCancel(ctx), schema)
	if err != nil {
		t.Logf("drop test schema %s: %v", schema, err)
	}
}

// RunTransactionalTest runs callback inside a transaction that is rolled back on
// cleanup. Sub-transactions resolve to that same transaction, so concurrent calls
// sharing the connection cannot deadlock.
func RunTransactionalTest(t *testing.T, config postgres.Config, callback TransactionalTestFunc) {
	t.Helper()

	ctx, err := postgres.NewContext(t.Context(), config)
	require.NoError(t, err)

	db, err := postgres.GetContext(ctx)
	require.NoError(t, err)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = tx.Rollback()
	})

	callback(postgres.WithTx(ctx, &passthroughTx{Tx: tx}), t)
}

const (
	// dbTestTemplatePrefix starts the name of the migrated template every test database is cloned
	// from. dbTestTemplateName appends a hash of the migration set, so a changed migration gets a
	// new template, and no test runs on an outdated schema.
	dbTestTemplatePrefix = "gotpl_"

	// dbTestInstancePrefix names the throwaway database of one RunDBTest call.
	dbTestInstancePrefix = "gotest_"

	// dbTestMaintenanceDatabase is the database the maintenance connection issues CREATE DATABASE and
	// DROP DATABASE from. Neither statement runs from the database it targets, and CREATE … TEMPLATE
	// needs its template free of sessions. The maintenance pool therefore points at an unrelated
	// database.
	dbTestMaintenanceDatabase = "postgres"

	// dbTestCreateAttempts bounds the attempts of CREATE DATABASE … TEMPLATE. The connections of a
	// previous clone can take a moment to drain, which fails the statement with SQLSTATE 55006
	// (object_in_use). A short retry absorbs that. A real configuration error fails every attempt.
	dbTestCreateAttempts = 5
	// dbTestCreateBackoff is the wait between CREATE DATABASE … TEMPLATE
	// attempts.
	dbTestCreateBackoff = 200 * time.Millisecond
)

// dbTestOptionsConfig is the capability RunDBTest needs beyond the Config interface. RunDBTest reads
// the raw driver options to connect to another database of the same cluster, such as the clone of
// a test. postgrespresets.Default satisfies it.
type dbTestOptionsConfig interface {
	Options() []pgdriver.Option
}

var (
	// dbTestMu serializes template creation within a process. The first RunDBTest call of a
	// migration set migrates its template while the other calls wait, so only the first test pays
	// the migration cost. The advisory lock in dbTestCreateTemplate covers other processes.
	dbTestMu sync.Mutex

	// dbTestTemplated records the templates this process created, keyed by template name and
	// target cluster. A failed creation stays unrecorded, so the next test retries it.
	dbTestTemplated = map[string]bool{}

	// dbTestCloneMu serializes CREATE DATABASE … TEMPLATE within a process.
	// PostgreSQL serializes CREATE DATABASE globally, so parallel clones only
	// produce contending statements and a retry storm. Taking them one at a time
	// leaves the bounded retry in dbTestCreateInstance to absorb cross-process
	// contention alone.
	dbTestCloneMu sync.Mutex
)

// RunDBTest runs callback against a fresh PostgreSQL database of its own, cloned from a migrated
// template with `CREATE DATABASE … TEMPLATE`. Each call is isolated, so the test and its sub-tests
// may run in parallel even when they reuse the same fixture keys.
//
// The migrations run once, into a template database named after their hash. Each test then gets a
// fast file-level copy of that template. The test's cleanup drops the copy. The template stays for
// later runs until the migrations change.
//
// config must expose Options() []pgdriver.Option, as postgrespresets.Default does. callback
// receives a context carrying a *bun.DB for its database, which [postgres.GetContext] returns.
func RunDBTest(t *testing.T, config postgres.Config, migrations fs.FS, callback TransactionalTestFunc) {
	t.Helper()

	optionsConfig, ok := config.(dbTestOptionsConfig)
	require.Truef(t, ok,
		"RunDBTest requires a Config exposing Options() []pgdriver.Option (e.g. postgrespresets.Default)")

	options := optionsConfig.Options()

	template, err := dbTestEnsureTemplate(t.Context(), options, migrations)
	require.NoError(t, err)

	instance := dbTestInstancePrefix + strings.ToLower(rand.Text())

	db, err := dbTestNewDatabase(t, options, instance, func(ctx context.Context, maintenance *bun.DB) error {
		return dbTestCreateInstance(ctx, maintenance, instance, template)
	})
	require.NoError(t, err)

	callback(context.WithValue(t.Context(), postgres.ContextKey{}, db), t)
}

// dbTestNewDatabase runs create on a maintenance connection to make the database called instance,
// then opens it. The test's cleanup closes the returned pool, then drops the database.
func dbTestNewDatabase(
	t *testing.T,
	options []pgdriver.Option,
	instance string,
	create func(ctx context.Context, maintenance *bun.DB) error,
) (*bun.DB, error) {
	t.Helper()

	maintenance, err := dbTestOpen(t.Context(), options, dbTestMaintenanceDatabase)
	if err != nil {
		return nil, err
	}

	err = create(t.Context(), maintenance)
	if err != nil {
		_ = maintenance.Close()

		return nil, err
	}

	// The drop is registered as soon as the database exists, so no later failure strands it.
	// Cleanups run last-in first-out, so the pool opened below closes before the drop runs.
	t.Cleanup(func() { dbTestDropInstance(options, instance) })

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

// dbTestDropInstance drops the database called instance from a test cleanup, on a maintenance
// connection of its own. It takes a background context, since t.Context is canceled by the time
// cleanups run.
func dbTestDropInstance(options []pgdriver.Option, instance string) {
	ctx := context.Background()

	maintenance, err := dbTestOpen(ctx, options, dbTestMaintenanceDatabase)
	if err != nil {
		return
	}
	defer func() { _ = maintenance.Close() }()

	_ = dbTestDropDatabase(ctx, maintenance, instance)
}

// dbTestEnsureTemplate returns the name of a migrated template database for the
// given migration set, creating and migrating it on first use. It serializes
// in-process callers on dbTestMu, so the migrations run once per process.
// dbTestCreateTemplate prevents the duplication across processes.
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

// dbTestCreateTemplate creates and migrates the template database called name, unless another test
// binary already did. It holds a session-level advisory lock meanwhile, so concurrent `go test`
// binaries cannot race on one template. The lock is keyed by a hash of name and held by the
// maintenance connection. Closing that connection releases the lock on every error path.
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
	// Closing the maintenance connection, deferred above, also releases the lock.
	defer func() { _, _ = maintenance.NewRaw("SELECT pg_advisory_unlock(?)", lockKey).Exec(ctx) }()

	var exists bool

	err = maintenance.NewRaw(
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = ?)", name,
	).Scan(ctx, &exists)
	if err != nil {
		return fmt.Errorf("probe template database: %w", err)
	}

	// A previous run already migrated the database of that name. The hash in the name ties it to the
	// current migration set, so it is reused as is.
	if exists {
		return nil
	}

	_, err = maintenance.NewRaw("CREATE DATABASE " + dbTestQuoteIdent(name)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("create template database: %w", err)
	}

	// The existence probe of a later call adopts any database under this name as
	// a valid template, so a partially migrated one is dropped.
	err = dbTestMigrateTemplate(ctx, options, name, migrations)
	if err != nil {
		_ = dbTestDropDatabase(ctx, maintenance, name)

		return err
	}

	return nil
}

// dbTestMigrateTemplate migrates the template database called name, then closes
// its pool. A database serves as a CREATE … TEMPLATE source only while no session
// is connected to it, so a pool that fails to close fails the template.
func dbTestMigrateTemplate(ctx context.Context, options []pgdriver.Option, name string, migrations fs.FS) error {
	templateDB, err := dbTestOpen(ctx, options, name)
	if err != nil {
		return err
	}

	err = postgres.RunMigrations(ctx, templateDB, migrations)
	if err != nil {
		_ = templateDB.Close()

		return fmt.Errorf("migrate template database: %w", err)
	}

	err = templateDB.Close()
	if err != nil {
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

// dbTestOpen opens a bun.DB on the named database, connecting as options otherwise describe.
// pgdriver applies options in order, so the trailing pgdriver.WithDatabase overrides the database
// that options select.
func dbTestOpen(ctx context.Context, options []pgdriver.Option, database string) (*bun.DB, error) {
	options = append(slices.Clone(options), pgdriver.WithDatabase(database))

	db := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(options...)), pgdialect.New(),
		bun.WithDiscardUnknownColumns())

	err := postgres.Ping(ctx, db)
	if err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("open test database %q: %w", database, err)
	}

	return db, nil
}

// dbTestTemplateName names the template database after a hash of the path and content of every
// migration file. A changed migration therefore gets a new template.
func dbTestTemplateName(migrations fs.FS) (string, error) {
	paths, err := migrationFiles(migrations)
	if err != nil {
		return "", fmt.Errorf("hash migrations: %w", err)
	}

	digest := sha256.New()

	for _, path := range paths {
		body, err := fs.ReadFile(migrations, path)
		if err != nil {
			return "", fmt.Errorf("hash migrations: %w", err)
		}

		// Each file contributes its length-prefixed path, then a fixed-size content
		// digest, so a shifting path/content boundary cannot give two distinct
		// migration sets the same byte stream.
		content := sha256.Sum256(body)

		hashField(digest, []byte(path))
		_, _ = digest.Write(content[:])
	}

	sum := digest.Sum(nil)

	return dbTestTemplatePrefix + hex.EncodeToString(sum[:8]), nil
}

// migrationFiles lists the paths of the files in migrations, in lexical order.
func migrationFiles(migrations fs.FS) ([]string, error) {
	var paths []string

	err := fs.WalkDir(migrations, ".", func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			paths = append(paths, path)
		}

		return err
	})

	return paths, err
}

// hashField writes field to digest behind its length, so consecutive fields keep their boundary.
func hashField(digest hash.Hash, field []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(field)))

	_, _ = digest.Write(size[:])
	_, _ = digest.Write(field)
}

// dbTestAdvisoryKey maps a database name to the int64 key used with
// pg_advisory_lock, so concurrent test binaries serialize template creation on
// the same value.
func dbTestAdvisoryKey(name string) int64 {
	sum := sha256.Sum256([]byte(name))

	return int64(binary.BigEndian.Uint64(sum[:8])) //nolint:gosec // wrap-around is fine for a lock key.
}

// dbTestQuoteIdent double-quotes a SQL identifier. A database name cannot be bound as a query
// parameter, so it is interpolated. The generated names use a restricted character set, and
// quoting them adds defense in depth.
func dbTestQuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
