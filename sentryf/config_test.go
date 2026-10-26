package sentryf_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/sentryf"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		env map[string]string

		expect sentryf.Config
	}{
		{
			name: "Success/Unset",
		},
		{
			name: "Success/Set",

			env: map[string]string{
				"SENTRY_DSN":         "https://public@o1.ingest.us.sentry.io/2",
				"SENTRY_RELEASE":     "service-authentication@2.17.0",
				"SENTRY_ENVIRONMENT": "production",
			},

			expect: sentryf.Config{
				DSN:         "https://public@o1.ingest.us.sentry.io/2",
				Release:     "service-authentication@2.17.0",
				Environment: "production",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			getenv := func(name string) string { return testCase.env[name] }

			require.Equal(t, testCase.expect, sentryf.LoadConfig(getenv))
		})
	}
}
