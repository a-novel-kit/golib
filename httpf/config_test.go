package httpf_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/httpf"
)

func TestLoadServerConfig(t *testing.T) {
	t.Parallel()

	// A service changes a default on a copy; an unset variable keeps it.
	custom := httpf.ServerConfigDefault
	custom.Timeouts.Shutdown = 25 * time.Second

	testCases := []struct {
		name string

		env      map[string]string
		defaults httpf.ServerConfig

		expect      httpf.ServerConfig
		expectPanic bool
	}{
		{
			name: "Success/Defaults",

			defaults: custom,

			expect: custom,
		},
		{
			name: "Success/Overrides",

			env: map[string]string{
				"REST_PORT":                   "9090",
				"REST_TIMEOUT_READ":           "1s",
				"REST_TIMEOUT_READ_HEADER":    "2s",
				"REST_TIMEOUT_WRITE":          "3s",
				"REST_TIMEOUT_IDLE":           "4s",
				"REST_TIMEOUT_REQUEST":        "5s",
				"REST_TIMEOUT_SHUTDOWN":       "6s",
				"REST_MAX_REQUEST_SIZE":       "1024",
				"REST_CLIENT_IP_HEADER":       "X-Client-IP",
				"REST_RATE_LIMITS_ENABLED":    "false",
				"REST_RATE_LIMIT":             "10/1m",
				"REST_CORS_ALLOWED_ORIGINS":   "https://a.test,https://b.test",
				"REST_CORS_ALLOWED_HEADERS":   "Authorization",
				"REST_CORS_ALLOW_CREDENTIALS": "true",
				"REST_CORS_MAX_AGE":           "60",
			},
			defaults: httpf.ServerConfigDefault,

			expect: httpf.ServerConfig{
				Port: 9090,
				Timeouts: httpf.ServerTimeouts{
					Read:       time.Second,
					ReadHeader: 2 * time.Second,
					Write:      3 * time.Second,
					Idle:       4 * time.Second,
					Request:    5 * time.Second,
					Shutdown:   6 * time.Second,
				},
				MaxRequestSize: 1024,
				ClientIPHeader: "X-Client-IP",
				RateLimit:      httpf.Rate{Requests: 10, Window: time.Minute},
				Cors: httpf.CorsConfig{
					AllowedOrigins:   []string{"https://a.test", "https://b.test"},
					AllowedHeaders:   []string{"Authorization"},
					AllowCredentials: true,
					MaxAge:           60,
				},
			},
		},
		{
			name: "Error/Unparsable",

			env:      map[string]string{"REST_RATE_LIMITS_ENABLED": "sometimes"},
			defaults: httpf.ServerConfigDefault,

			expectPanic: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			getenv := func(name string) string { return testCase.env[name] }

			if testCase.expectPanic {
				require.Panics(t, func() { httpf.LoadServerConfig(getenv, testCase.defaults) })

				return
			}

			require.Equal(t, testCase.expect, httpf.LoadServerConfig(getenv, testCase.defaults))
		})
	}
}
