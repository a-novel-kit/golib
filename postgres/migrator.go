package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"

	"github.com/a-novel-kit/golib/otel"
)

// ErrNoDbInContext is returned by RunMigrationsContext when the context carries
// a bun.IDB that is not a full *bun.DB. Migrations need a database handle, not a
// transaction; [ErrNoIDBInContext] covers the case of no connection at all.
var ErrNoDbInContext = errors.New("context does not contain a bun.DB")

// RunMigrations runs all the migrations found in the provided filesystem.
func RunMigrations(ctx context.Context, db *bun.DB, migrations fs.FS) error {
	ctx, span := otel.Tracer().Start(ctx, "postgres.RunMigrations")
	defer span.End()

	mig := migrate.NewMigrations()

	err := mig.Discover(migrations)
	if err != nil {
		return otel.ReportError(span, fmt.Errorf("discover mig: %w", err))
	}

	migrator := migrate.NewMigrator(db, mig)

	err = migrator.Init(ctx)
	if err != nil {
		return otel.ReportError(span, fmt.Errorf("create migrator: %w", err))
	}

	_, err = migrator.Migrate(ctx)
	if err != nil {
		return otel.ReportError(span, fmt.Errorf("apply mig: %w", err))
	}

	otel.ReportSuccessNoContent(span)

	return nil
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
