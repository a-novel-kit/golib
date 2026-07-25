package postgres

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
)

func roundtripTestDB(t *testing.T) *bun.DB {
	t.Helper()

	dsn := os.Getenv("POSTGRES_DSN")
	require.NotEmpty(t, dsn,
		"POSTGRES_DSN must point at a throwaway database — the roundtrip migrates a real one")

	db, err := newRoundtripDatabase(t, []pgdriver.Option{pgdriver.WithDSN(dsn)})
	require.NoError(t, err)

	return db
}

func TestVerifyRoundtripAcceptsAReversibleSet(t *testing.T) {
	t.Parallel()

	err := verifyRoundtrip(t.Context(), roundtripTestDB(t),
		os.DirFS("testdata/roundtrip/migrations"),
		&RoundtripOptions{Fixtures: os.DirFS("testdata/roundtrip/fixtures")})

	require.NoError(t, err)
}

func TestVerifyRoundtripCatchesAnObjectLeftBehind(t *testing.T) {
	t.Parallel()

	err := verifyRoundtrip(t.Context(), roundtripTestDB(t),
		os.DirFS("testdata/roundtrip-broken/migrations"), nil)

	require.ErrorIs(t, err, errMigrationDrift)
	require.Contains(t, err.Error(), "20260725000002_probe_status",
		"the failure must name the migration whose down is at fault")
	require.Contains(t, err.Error(), "roundtrip_status",
		"the failure must name the object left behind")
}

func TestVerifyRoundtripAppliesFixturesBeforeTheNextMigration(t *testing.T) {
	t.Parallel()

	migrations := os.DirFS("testdata/roundtrip-strict/migrations")

	t.Run("passes against an empty table", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, verifyRoundtrip(t.Context(), roundtripTestDB(t), migrations, nil))
	})

	t.Run("fails once the previous migration's fixture has seeded a row", func(t *testing.T) {
		t.Parallel()

		err := verifyRoundtrip(t.Context(), roundtripTestDB(t), migrations,
			&RoundtripOptions{Fixtures: os.DirFS("testdata/roundtrip/fixtures")})

		require.Error(t, err)
		require.Contains(t, err.Error(), "20260725000002_probe_required")
		require.Contains(t, err.Error(), "contains null values")
	})
}

//nolint:paralleltest // t.Setenv is incompatible with t.Parallel.
func TestVerifyRoundtripSnapshots(t *testing.T) {
	migrations := os.DirFS("testdata/roundtrip/migrations")

	t.Run("records then re-reads them", func(t *testing.T) {
		dir := t.TempDir()

		t.Setenv(roundtripUpdateEnv, "1")
		require.NoError(t, verifyRoundtrip(t.Context(), roundtripTestDB(t), migrations,
			&RoundtripOptions{Snapshots: dir}))

		recorded, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, recorded, 3, "one snapshot per migration")

		first, err := os.ReadFile(filepath.Join(dir, "20260725000001_probe_table.txt")) //nolint:gosec // t.TempDir.
		require.NoError(t, err)
		require.Contains(t, string(first), roundtripHistoryRecord)

		t.Setenv(roundtripUpdateEnv, "")
		require.NoError(t, verifyRoundtrip(t.Context(), roundtripTestDB(t), migrations,
			&RoundtripOptions{Snapshots: dir}))
	})

	t.Run("rejects a snapshot that no longer matches", func(t *testing.T) {
		dir := t.TempDir()

		t.Setenv(roundtripUpdateEnv, "1")
		require.NoError(t, verifyRoundtrip(t.Context(), roundtripTestDB(t), migrations,
			&RoundtripOptions{Snapshots: dir}))

		t.Setenv(roundtripUpdateEnv, "")

		edited := filepath.Join(dir, "20260725000001_probe_table.txt")
		require.NoError(t, os.WriteFile(edited, []byte("relation\troundtrip_probe\tr\n"), 0o600))

		err := verifyRoundtrip(t.Context(), roundtripTestDB(t), migrations,
			&RoundtripOptions{Snapshots: dir})

		require.ErrorIs(t, err, errMigrationDrift)
		require.Contains(t, err.Error(), "20260725000001_probe_table")
	})

	t.Run("reports a snapshot that was never recorded", func(t *testing.T) {
		err := verifyRoundtrip(t.Context(), roundtripTestDB(t), migrations,
			&RoundtripOptions{Snapshots: t.TempDir()})

		require.ErrorIs(t, err, os.ErrNotExist)
		require.Contains(t, err.Error(), roundtripUpdateEnv,
			"the failure must say how to record the missing snapshot")
	})
}

func TestVerifyRoundtripRejectsAnEmptyMigrationSet(t *testing.T) {
	t.Parallel()

	err := verifyRoundtrip(t.Context(), roundtripTestDB(t), os.DirFS(t.TempDir()), nil)

	require.ErrorContains(t, err, "no migrations discovered")
}

func TestRoundtripPrefixNarrowsTheSetToOnePendingMigration(t *testing.T) {
	t.Parallel()

	ordered := migrate.MigrationSlice{
		{Name: "20260725000001", Comment: "first"},
		{Name: "20260725000002", Comment: "second"},
		{Name: "20260725000003", Comment: "third"},
	}

	require.Len(t, roundtripPrefix(ordered, 1).Sorted(), 1)
	require.Len(t, roundtripPrefix(ordered, 2).Sorted(), 2)
	require.Equal(t, "20260725000002", roundtripPrefix(ordered, 2).Sorted()[1].Name)
}

func TestRoundtripSlug(t *testing.T) {
	t.Parallel()

	require.Equal(t, "20260725000001_probe_table",
		roundtripSlug(migrate.Migration{Name: "20260725000001", Comment: "probe_table"}))
	require.Equal(t, "20260725000001",
		roundtripSlug(migrate.Migration{Name: "20260725000001"}))
}

func TestRoundtripHistoryDigests(t *testing.T) {
	t.Parallel()

	migrations := fstest.MapFS{
		"20260725000001_first.up.sql":    {Data: []byte("SELECT 1;")},
		"20260725000001_first.down.sql":  {Data: []byte("SELECT 2;")},
		"20260725000002_second.up.sql":   {Data: []byte("SELECT 3;")},
		"20260725000002_second.down.sql": {Data: []byte("SELECT 4;")},
	}
	ordered := migrate.MigrationSlice{
		{Name: "20260725000001", Comment: "first"},
		{Name: "20260725000002", Comment: "second"},
	}

	baseline, err := roundtripHistoryDigests(migrations, ordered)
	require.NoError(t, err)
	require.Len(t, baseline, 2)

	for _, sourcePath := range []string{
		"20260725000001_first.up.sql",
		"20260725000001_first.down.sql",
	} {
		t.Run(sourcePath, func(t *testing.T) {
			t.Parallel()

			edited := maps.Clone(migrations)
			edited[sourcePath] = &fstest.MapFile{Data: []byte("SELECT 5;")}

			changed, err := roundtripHistoryDigests(edited, ordered)
			require.NoError(t, err)
			require.NotEqual(t, baseline[0], changed[0])
			require.NotEqual(t, baseline[1], changed[1])
		})
	}

	appended := maps.Clone(migrations)
	appended["20260725000003_third.up.sql"] = &fstest.MapFile{Data: []byte("SELECT 6;")}
	appended["20260725000003_third.down.sql"] = &fstest.MapFile{Data: []byte("SELECT 7;")}

	withAppend, err := roundtripHistoryDigests(appended, append(ordered,
		migrate.Migration{Name: "20260725000003", Comment: "third"}))
	require.NoError(t, err)
	require.Equal(t, baseline, withAppend[:2])
}
