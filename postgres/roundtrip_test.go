package postgres_test

import (
	"os"
	"testing"

	"github.com/a-novel-kit/golib/postgres"
)

func TestRunMigrationRoundtripTest(t *testing.T) {
	t.Parallel()

	postgres.RunMigrationRoundtripTest(t, testConfig(t),
		os.DirFS("testdata/roundtrip/migrations"),
		&postgres.RoundtripOptions{Fixtures: os.DirFS("testdata/roundtrip/fixtures")})
}
