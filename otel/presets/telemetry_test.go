package otelpresets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	otelpresets "github.com/a-novel-kit/golib/otel/presets"
)

func TestLoadTelemetry(t *testing.T) {
	testCases := []struct {
		name string

		otel     string
		endpoint string

		expect otelpresets.Telemetry
	}{
		{
			name: "Success/Disabled",

			endpoint: "http://collector:4318",

			expect: otelpresets.Telemetry{Otel: &otelpresets.Disabled{}},
		},
		{
			name: "Success/Local",

			otel: "true",

			expect: otelpresets.Telemetry{Otel: &otelpresets.Local{FlushTimeout: otelpresets.ServerFlushTimeout}},
		},
		{
			// The server spans record each request, so no request log is written.
			name: "Success/OTLP",

			otel:     "true",
			endpoint: "http://collector:4318",

			expect: otelpresets.Telemetry{
				Otel:      &otelpresets.OTLP{FlushTimeout: otelpresets.ServerFlushTimeout},
				Logger:    &loggingpresets.LogOTel{},
				RPCLogger: &loggingpresets.GRPCOTel{},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", testCase.endpoint)

			getenv := func(name string) string { return map[string]string{"OTEL": testCase.otel}[name] }
			telemetry := otelpresets.LoadTelemetry(getenv, otelpresets.ServerFlushTimeout)

			require.Equal(t, testCase.expect.Otel, telemetry.Otel)

			// The terminal loggers write to the same output.
			if testCase.expect.Logger == nil {
				terminal, ok := telemetry.Logger.(*loggingpresets.LogLocal)
				require.True(t, ok)
				require.Equal(t, &loggingpresets.HTTPLocal{BaseLogger: terminal}, telemetry.HTTPLogger)
				require.Equal(t, &loggingpresets.GRPCLocal{}, telemetry.RPCLogger)

				return
			}

			require.Equal(t, testCase.expect, telemetry)
		})
	}

	t.Run("Error/Unparsable", func(t *testing.T) {
		require.Panics(t, func() {
			otelpresets.LoadTelemetry(func(string) string { return "on" }, otelpresets.ServerFlushTimeout)
		})
	})
}
