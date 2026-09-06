package postgrestest_test

import (
	"context"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/a-novel-kit/golib/postgres"
	"github.com/a-novel-kit/golib/postgres/postgrestest"
)

var (
	_ func(context.Context, postgres.Config) (context.Context, string, error) = postgrestest.NewContextTest
	_ func(
		*testing.T,
		postgres.Config,
		fs.FS,
		postgrestest.TransactionalTestFunc,
	) = postgrestest.RunIsolatedTransactionalTest
	_ func(*testing.T, postgres.Config, postgrestest.TransactionalTestFunc)        = postgrestest.RunTransactionalTest
	_ func(*testing.T, postgres.Config, fs.FS, postgrestest.TransactionalTestFunc) = postgrestest.RunDBTest
	_ func(*testing.T, postgres.Config, fs.FS, *postgrestest.RoundtripOptions)     = postgrestest.RunMigrationRoundtripTest
	_ func(context.Context, bun.IDB, string) (string, error)                       = postgrestest.SchemaSnapshot
)

func TestSnapshotDelta(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"missing: removed", "unexpected: added"},
		postgrestest.SnapshotDelta("kept\nremoved\n", "kept\nadded\n"))
	require.Error(t, postgrestest.ErrUnsupportedSchemaObject)
}
