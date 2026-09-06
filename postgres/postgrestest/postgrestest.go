// Package postgrestest provides PostgreSQL test harnesses backed by postgres.
package postgrestest

import (
	"context"
	"io/fs"
	"testing"

	"github.com/uptrace/bun"

	"github.com/a-novel-kit/golib/postgres"
)

// TransactionalTestFunc is the body of a database-backed test, run with a
// context carrying the connection isolated for that test.
//
//nolint:staticcheck // This additive alias forwards to the legacy type until consumers migrate.
type TransactionalTestFunc = postgres.TransactionalTestFunc

// RoundtripOptions configures [RunMigrationRoundtripTest]. Its zero value is valid.
//
//nolint:staticcheck // This additive alias forwards to the legacy type until consumers migrate.
type RoundtripOptions = postgres.RoundtripOptions

// ErrUnsupportedSchemaObject reports a schema object the census cannot safely compare.
//
//nolint:staticcheck // This additive variable preserves the legacy sentinel until consumers migrate.
var ErrUnsupportedSchemaObject = postgres.ErrUnsupportedSchemaObject

// NewContextTest derives a context bound to a fresh test schema created through config.
func NewContextTest(ctx context.Context, config postgres.Config) (context.Context, string, error) {
	//nolint:staticcheck // This additive facade forwards to the legacy helper until consumers migrate.
	return postgres.NewContextTest(ctx, config)
}

// RunIsolatedTransactionalTest runs callback in a throwaway schema.
func RunIsolatedTransactionalTest(
	t *testing.T,
	config postgres.Config,
	migrations fs.FS,
	callback TransactionalTestFunc,
) {
	t.Helper()

	//nolint:staticcheck // This additive facade forwards to the legacy helper until consumers migrate.
	postgres.RunIsolatedTransactionalTest(t, config, migrations, callback)
}

// RunTransactionalTest runs callback in a transaction rolled back during test cleanup.
func RunTransactionalTest(t *testing.T, config postgres.Config, callback TransactionalTestFunc) {
	t.Helper()

	//nolint:staticcheck // This additive facade forwards to the legacy helper until consumers migrate.
	postgres.RunTransactionalTest(t, config, callback)
}

// RunDBTest runs callback against a migrated, disposable PostgreSQL database.
func RunDBTest(t *testing.T, config postgres.Config, migrations fs.FS, callback TransactionalTestFunc) {
	t.Helper()

	//nolint:staticcheck // This additive facade forwards to the legacy helper until consumers migrate.
	postgres.RunDBTest(t, config, migrations, callback)
}

// RunMigrationRoundtripTest proves every down migration reverses its matching up migration.
func RunMigrationRoundtripTest(t *testing.T, config postgres.Config, migrations fs.FS, opts *RoundtripOptions) {
	t.Helper()

	//nolint:staticcheck // This additive facade forwards to the legacy helper until consumers migrate.
	postgres.RunMigrationRoundtripTest(t, config, migrations, opts)
}

// SchemaSnapshot renders supported schema objects as canonical records.
func SchemaSnapshot(ctx context.Context, db bun.IDB, schema string) (string, error) {
	//nolint:staticcheck // This additive facade forwards to the legacy helper until consumers migrate.
	return postgres.SchemaSnapshot(ctx, db, schema)
}

// SnapshotDelta returns sorted per-object differences between two schema snapshots.
func SnapshotDelta(want, got string) []string {
	//nolint:staticcheck // This additive facade forwards to the legacy helper until consumers migrate.
	return postgres.SnapshotDelta(want, got)
}
