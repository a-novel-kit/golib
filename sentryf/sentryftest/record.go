package sentryftest

import (
	"testing"

	"github.com/getsentry/sentry-go"

	"github.com/a-novel-kit/golib/sentryf"
)

// Release and Environment name the build and the deployment of every event [Record] keeps.
const (
	Release     = "golib@0.0.0-test"
	Environment = "test"
)

// dsn names a project on a reserved host. The recording transport never contacts it.
const dsn = "https://public@sentry.invalid/1"

// Record installs a reporter that keeps its events in memory, and removes it once t ends. The
// reporter is process-wide, so a test that records cannot run beside another one that captures.
func Record(t *testing.T) *sentry.MockTransport {
	t.Helper()

	transport := &sentry.MockTransport{}

	err := sentryf.Init(sentryf.Config{DSN: dsn, Release: Release, Environment: Environment, Transport: transport})
	if err != nil {
		panic(err)
	}

	t.Cleanup(func() {
		// Without a DSN, Init only removes the reporter, which cannot fail.
		_ = sentryf.Init(sentryf.Config{})
	})

	return transport
}
