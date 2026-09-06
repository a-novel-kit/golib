package postgres_test

import (
	"os"
	"testing"

	"github.com/a-novel-kit/golib/postgres/postgrestest"
)

func TestRunMigrationRoundtripTest(t *testing.T) {
	t.Parallel()

	postgrestest.RunMigrationRoundtripTest(t, testConfig(t),
		os.DirFS("postgrestest/testdata/roundtrip/migrations"),
		&postgrestest.RoundtripOptions{Fixtures: os.DirFS("postgrestest/testdata/roundtrip/fixtures")})
}
