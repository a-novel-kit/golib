package grpcf_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/grpcf"
	"github.com/a-novel-kit/golib/servertls"
)

func TestLoadServerConfig(t *testing.T) {
	t.Parallel()

	// A service changes a default on a copy; an unset variable keeps it.
	custom := grpcf.ServerConfigDefault
	custom.Shutdown = 8 * time.Second

	testCases := []struct {
		name string

		env      map[string]string
		defaults grpcf.ServerConfig

		expect      grpcf.ServerConfig
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
				"GRPC_PORT":                 "9090",
				"GRPC_TLS_CERT_FILE":        "/etc/tls/tls.crt",
				"GRPC_TLS_KEY_FILE":         "/etc/tls/tls.key",
				"GRPC_PLAINTEXT_PORT":       "8080",
				"GRPC_TIMEOUT_SHUTDOWN":     "3s",
				"GRPC_MAX_CONCURRENT_CALLS": "20",
			},
			defaults: grpcf.ServerConfigDefault,

			expect: grpcf.ServerConfig{
				Port:               9090,
				TLS:                servertls.Files{CertFile: "/etc/tls/tls.crt", KeyFile: "/etc/tls/tls.key"},
				PlaintextPort:      8080,
				Shutdown:           3 * time.Second,
				MaxConcurrentCalls: 20,
			},
		},
		{
			name: "Error/Unparsable",

			env:      map[string]string{"GRPC_PORT": "port"},
			defaults: grpcf.ServerConfigDefault,

			expectPanic: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			getenv := func(name string) string { return testCase.env[name] }

			if testCase.expectPanic {
				require.Panics(t, func() { grpcf.LoadServerConfig(getenv, testCase.defaults) })

				return
			}

			require.Equal(t, testCase.expect, grpcf.LoadServerConfig(getenv, testCase.defaults))
		})
	}
}
