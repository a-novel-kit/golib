package otelpresets_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	otelpresets "github.com/a-novel-kit/golib/otel/presets"
)

//nolint:paralleltest // Credential and resource cases change process-wide environment variables.
func TestGcloud(t *testing.T) {
	testCases := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "Success/StandardPropagation",
			test: func(t *testing.T) {
				t.Helper()

				config := &otelpresets.Gcloud{}
				propagators, err := config.GetPropagators()
				require.NoError(t, err)
				require.ElementsMatch(t, []string{"traceparent", "tracestate", "baggage"}, propagators.Fields())
			},
		},
		{
			name: "Error/MissingCredentials",
			test: func(t *testing.T) {
				t.Helper()
				t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "missing.json"))

				config := &otelpresets.Gcloud{}
				provider, err := config.GetTraceProvider()

				require.ErrorContains(t, err, "load Google application default credentials")
				require.Nil(t, provider)
			},
		},
		{
			name: "Success/TraceProvider",
			test: func(t *testing.T) {
				t.Helper()
				setGoogleApplicationCredentials(t)
				t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
				t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "gcp.project_id=ambient-project,service.name=test-service")

				config := &otelpresets.Gcloud{ProjectID: "configured-project"}
				provider, err := config.GetTraceProvider()
				require.NoError(t, err)

				traceProvider, ok := provider.(*sdktrace.TracerProvider)
				require.True(t, ok)

				_, span := traceProvider.Tracer("gcloud-test").Start(context.Background(), "inspect-resource")
				readWriteSpan, ok := span.(sdktrace.ReadWriteSpan)
				require.True(t, ok)

				projectID, found := readWriteSpan.Resource().Set().Value(attribute.Key("gcp.project_id"))

				require.NoError(t, traceProvider.Shutdown(context.Background()))
				span.End()
				require.True(t, found)
				require.Equal(t, "configured-project", projectID.AsString())
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, testCase.test)
	}
}

func setGoogleApplicationCredentials(t *testing.T) {
	t.Helper()

	credentialsPath := filepath.Join(t.TempDir(), "credentials.json")

	err := os.WriteFile(credentialsPath, []byte(`{
		"type": "authorized_user",
		"client_id": "test-client",
		"client_secret": "test-secret",
		"refresh_token": "test-refresh-token"
	}`), 0o600)
	if err != nil {
		panic(err)
	}

	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath)
}
