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

// ErrNoDbInContext is returned by RunMigrationsContext when the context carries
// a bun.IDB that is not a full *bun.DB, which migrations require.
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

	conn, err := db.Conn(ctx)
	if err != nil {
		return otel.ReportError(span, fmt.Errorf("open lock connection: %w", err))
	}

	// The session holds the lock and runs with lock_timeout lifted, so it never goes back to the
	// pool: closing it releases the lock. PostgreSQL releases it the same way if the runner dies.
	defer func() {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
	}()

	_, err = conn.ExecContext(ctx, migrationLockQuery)
	if err != nil {
		return otel.ReportError(span, fmt.Errorf("lock migrations: %w", err))
	}

	migrator := migrate.NewMigrator(db, mig, migrate.WithMarkAppliedOnSuccess(true))

	err = migrator.Init(ctx)
	if err != nil {
		return otel.ReportError(span, fmt.Errorf("create migrator: %w", err))
	}

	for attempt := 1; ; attempt++ {
		_, err = migrator.Migrate(ctx)
		if err == nil || attempt == lockAttempts || !lockTimedOut(err) {
			break
		}

		select {
		case <-ctx.Done():
			return otel.ReportError(span, fmt.Errorf("apply mig: %w", errors.Join(err, ctx.Err())))
		case <-time.After(lockRetryDelay):
		}
	}

	if err != nil {
		return otel.ReportError(span, fmt.Errorf("apply mig: %w", err))
	}

	return nil
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
