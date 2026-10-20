package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"

	"github.com/a-novel-kit/golib/otel"
)

// ErrNoDbInContext is returned by an operation that needs the connection pool, such as
// [RunMigrationsContext] or [Health], when the context carries a transaction instead.
// [ErrNoIDBInContext] covers the case of no connection at all.
var ErrNoDbInContext = errors.New("context does not contain a bun.DB")

const (
	// lockAttempts bounds the runs a migration gets when it gives up waiting for a lock, and
	// lockRetryDelay spaces them, so the transaction it waited on can finish first.
	lockAttempts   = 5
	lockRetryDelay = 2 * time.Second

	// lockNotAvailable is the SQLSTATE a statement fails with once it waited lock_timeout for a lock.
	lockNotAvailable = "55P03"

	// migrationLockQuery takes a session lock on the migration history of the connection's schema,
	// lifting lock_timeout first: a runner waits out another one, however long its migrations run.
	migrationLockQuery = `SET lock_timeout = 0;
SELECT pg_advisory_lock(hashtextextended('golib/migrations:' || current_schema(), 0));`
)

// RunMigrations runs all the migrations found in the provided filesystem.
//
// Runners against one schema apply migrations one at a time: a runner waits for another to finish,
// then applies what is left. A runner keeps one connection of the pool for that wait, so the pool
// needs room for two. A migration is recorded once it succeeds, so a failed one runs again
// next time. PostgreSQL runs a migration file as a single transaction, so a failure leaves nothing
// of it behind, unless the file is split with --bun:split.
//
// Give the connection a lock_timeout, so a migration waiting for a lock a long transaction holds
// gives up instead of queueing every query on its table behind it. A migration that gives up this
// way is run again after a pause, a few times.
func RunMigrations(ctx context.Context, db *bun.DB, migrations fs.FS) error {
	ctx, span := otel.Tracer().Start(ctx, "postgres.RunMigrations")
	defer span.End()

	mig := migrate.NewMigrations()

	err := mig.Discover(migrations)
	if err != nil {
		return otel.ReportError(span, fmt.Errorf("discover mig: %w", err))
	}

	release, err := lockMigrations(ctx, db)
	if err != nil {
		return otel.ReportError(span, err)
	}
	defer release()

	migrator := migrate.NewMigrator(db, mig, migrate.WithMarkAppliedOnSuccess(true))

	err = migrator.Init(ctx)
	if err != nil {
		return otel.ReportError(span, fmt.Errorf("create migrator: %w", err))
	}

	err = applyMigrations(ctx, migrator)
	if err != nil {
		return otel.ReportError(span, fmt.Errorf("apply mig: %w", err))
	}

	return nil
}

// lockMigrations takes the migration lock of the connection's schema on a session of its own, and
// returns the function that releases it.
func lockMigrations(ctx context.Context, db *bun.DB) (func(), error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open lock connection: %w", err)
	}

	_, err = conn.ExecContext(ctx, migrationLockQuery)
	if err != nil {
		discardConn(conn)

		return nil, fmt.Errorf("lock migrations: %w", err)
	}

	// The session runs with lock_timeout lifted, so it never goes back to the pool. Closing it
	// releases the lock, as PostgreSQL does if the runner dies.
	return func() { discardConn(conn) }, nil
}

// discardConn closes conn without returning it to the pool, which ends its session.
func discardConn(conn bun.Conn) {
	// Raw reporting driver.ErrBadConn marks the connection bad, so Close discards it.
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

// applyMigrations applies the pending migrations. A migration that gives up waiting for a lock
// runs again after lockRetryDelay, up to lockAttempts runs in all.
func applyMigrations(ctx context.Context, migrator *migrate.Migrator) error {
	for attempt := 1; ; attempt++ {
		_, err := migrator.Migrate(ctx)
		if err == nil || attempt == lockAttempts || !lockTimedOut(err) {
			return err
		}

		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-time.After(lockRetryDelay):
		}
	}
}

// lockTimedOut reports whether a statement gave up waiting for a lock after lock_timeout.
func lockTimedOut(err error) bool {
	var pgErr pgdriver.Error

	return errors.As(err, &pgErr) && pgErr.Field('C') == lockNotAvailable
}

// RunMigrationsContext runs all the migrations found in the provided filesystem,
// using the database connection from the context.
func RunMigrationsContext(ctx context.Context, migrations fs.FS) error {
	tx, err := GetContext(ctx)
	if err != nil {
		return fmt.Errorf("get db from context: %w", err)
	}

	db, ok := tx.(*bun.DB)
	if !ok {
		return ErrNoDbInContext
	}

	return RunMigrations(ctx, db, migrations)
}
