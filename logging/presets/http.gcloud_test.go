package loggingpresets_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
)

func TestHTTPGcloud(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		status int
	}{
		{name: "Success", status: http.StatusOK},
		{name: "Error/Server", status: http.StatusInternalServerError},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

			router := http.NewServeMux()
			router.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
			})

			logger := &loggingpresets.HTTPGcloud{BaseLogger: &loggingpresets.LogGcloud{ProjectId: "project"}}
			handler := otelhttp.NewMiddleware("", otelhttp.WithTracerProvider(provider))(logger.Logger()(router))

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/items/42", nil))

			require.Equal(t, testCase.status, w.Code)

			// The server span is the only span, and it reads the route the router matched.
			spans := recorder.Ended()
			require.Len(t, spans, 1)
			require.Equal(t, "GET /items/{id}", spans[0].Name())
		})
	}
}
