package postgrestest_test

import (
	"os"
	"testing"

	"github.com/a-novel-kit/golib/internal/pgtest"
	"github.com/a-novel-kit/golib/postgres/postgrestest"
)

func TestRunMigrationRoundtripTest(t *testing.T) {
	t.Parallel()

	postgrestest.RunMigrationRoundtripTest(t, pgtest.Config(t),
		os.DirFS("../testdata/roundtrip/migrations"),
		&postgrestest.RoundtripOptions{Fixtures: os.DirFS("../testdata/roundtrip/fixtures")})
}
