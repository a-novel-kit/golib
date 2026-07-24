package postgres_test

import (
	"os"
	"testing"

	"github.com/a-novel-kit/golib/postgres"
)

// The exported entry point is what services call, so it is exercised the way they will: a preset
// config, an embedded migration set, and fixtures beside it. Everything it can report is tested
// from inside the package, since failing the test is all a caller ever sees of it.
func TestRunMigrationRoundtripTest(t *testing.T) {
	t.Parallel()

	postgres.RunMigrationRoundtripTest(t, testConfig(t),
		os.DirFS("testdata/roundtrip/migrations"),
		&postgres.RoundtripOptions{Fixtures: os.DirFS("testdata/roundtrip/fixtures")})
}
