package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/postgres"
)

// schemaExists reports whether a schema is present, read through the primary connection
// so it is independent of the per-schema pool under test.
func schemaExists(ctx context.Context, t *testing.T, config postgres.Config, schema string) bool {
	t.Helper()

	db, err := config.DB(ctx)
	require.NoError(t, err)

	var count int

	err = db.NewRaw(
		"SELECT count(*) FROM information_schema.schemata WHERE schema_name = ?0", schema,
	).Scan(ctx, &count)
	require.NoError(t, err)

	return count > 0
}

// The isolated helper stands up a randomly named schema per call. Without a drop, both
// the schema and its cached pool live forever. This pins that the schema is gone once the
// test finishes.
func TestRunIsolatedTransactionalTestDropsItsSchema(t *testing.T) {
	t.Parallel()

	config := testConfig(t)

	var schema string

	// Registered before the helper, so by cleanup LIFO it runs after the helper's own
	// drop — this is the assertion that the drop happened.
	t.Cleanup(func() {
		require.NotEmpty(t, schema)
		require.False(t, schemaExists(context.Background(), t, config, schema),
			"the schema must be dropped after the isolated test")
	})

	postgres.RunIsolatedTransactionalTest(t, config, migrations, func(ctx context.Context, t *testing.T) {
		t.Helper()

		db, err := postgres.GetContext(ctx)
		require.NoError(t, err)

		require.NoError(t, db.NewRaw("SELECT current_schema()").Scan(ctx, &schema))
		require.True(t, schemaExists(ctx, t, config, schema), "schema must exist during the test")
	})
}

func TestDropSchemaEvictsTheCachedPool(t *testing.T) {
	t.Parallel()

	config := testConfig(t)

	dropper, ok := config.(interface {
		DropSchema(ctx context.Context, schema string) error
	})
	require.True(t, ok, "the Default preset must offer DropSchema")

	ctx := context.Background()

	_, schema, err := postgres.NewContextTest(ctx, config)
	require.NoError(t, err)
	require.True(t, schemaExists(ctx, t, config, schema))

	require.NoError(t, dropper.DropSchema(ctx, schema))
	require.False(t, schemaExists(ctx, t, config, schema))

	// A second drop is a no-op: the pool is gone from the cache and DROP ... IF EXISTS
	// tolerates the absent schema.
	require.NoError(t, dropper.DropSchema(ctx, schema))

	// Requesting the schema again opens a fresh pool, proving the old one was evicted
	// rather than handed back closed.
	reopened, err := config.DBSchema(ctx, schema, true)
	require.NoError(t, err)
	require.NoError(t, reopened.PingContext(ctx))

	require.NoError(t, dropper.DropSchema(ctx, schema))
}
