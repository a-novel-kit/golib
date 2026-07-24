package httpftest

import (
	"os"
	"path/filepath"
	"testing"
)

// Golden reads a fixture from the calling package's testdata directory.
//
// Store the bodies a [Server] replays pretty-printed, so a change to one reads as the lines that
// changed.
func Golden(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // Test-supplied path.
	if err != nil {
		panic(err)
	}

	return string(data)
}
