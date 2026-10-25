package loggingpresets_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

	// The cases run on a real server, because httptest.ResponseRecorder records an informational status
	// as the final one.
	testCases := []struct {
		name string

		handler http.HandlerFunc

		expectStatus   int
		expectSeverity string
		expectSize     string
	}{
		{
			name: "Success/ImplicitOK",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "done")
			},
			expectStatus:   http.StatusOK,
			expectSeverity: "INFO",
			expectSize:     "4",
		},
		{
			name: "Success/Created",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
			},
			expectStatus:   http.StatusCreated,
			expectSeverity: "INFO",
			expectSize:     "0",
		},
		{
			// net/http sends the first final status and drops the second.
			name: "Success/RepeatedWriteHeader",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectStatus:   http.StatusCreated,
			expectSeverity: "INFO",
			expectSize:     "0",
		},
		{
			// The first write sends an implicit 200, which a later WriteHeader cannot change.
			name: "Success/WriteHeaderAfterWrite",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "done")
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectStatus:   http.StatusOK,
			expectSeverity: "INFO",
			expectSize:     "4",
		},
		{
			name: "Success/Informational",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusEarlyHints)
				w.WriteHeader(http.StatusNoContent)
			},
			expectStatus:   http.StatusNoContent,
			expectSeverity: "INFO",
			expectSize:     "0",
		},
		{
			name: "Error/Server",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectStatus:   http.StatusInternalServerError,
			expectSeverity: "ERROR",
			expectSize:     "0",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

			router := http.NewServeMux()
			router.HandleFunc("GET /items/{id}", testCase.handler)

			out := &bytes.Buffer{}
			logger := &loggingpresets.HTTPGcloud{BaseLogger: &loggingpresets.LogGcloud{ProjectID: "project", Out: out}}
			server := httptest.NewServer(otelhttp.NewMiddleware("", otelhttp.WithTracerProvider(provider))(
				logger.Logger()(router),
			))
			t.Cleanup(server.Close)

			request, err := http.NewRequestWithContext(
				t.Context(), http.MethodGet, server.URL+"/items/42?email=jane.doe@example.com", nil,
			)
			require.NoError(t, err)

			response, err := server.Client().Do(request)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())

			// Close waits for the handler to return, so the span and the log entry are complete.
			server.Close()

			require.Equal(t, testCase.expectStatus, response.StatusCode)

			// The server span is the only span, and it reads the route the router matched.
			spans := recorder.Ended()
			require.Len(t, spans, 1)
			require.Equal(t, "GET /items/{id}", spans[0].Name())

			// The access log reports the response the client received. It is attached to that span,
			// and leaves the query out.
			var entry struct {
				Severity    string `json:"severity"`
				Trace       string `json:"logging.googleapis.com/trace"`
				SpanID      string `json:"logging.googleapis.com/spanId"`
				HTTPRequest struct {
					RequestURL   string `json:"requestUrl"`
					Status       int    `json:"status"`
					ResponseSize string `json:"responseSize"`
				} `json:"httpRequest"`
			}

			require.NoError(t, json.Unmarshal(out.Bytes(), &entry))
			require.Equal(t, testCase.expectStatus, entry.HTTPRequest.Status)
			require.Equal(t, testCase.expectSeverity, entry.Severity)
			require.Equal(t, testCase.expectSize, entry.HTTPRequest.ResponseSize)
			require.Equal(t, "projects/project/traces/"+spans[0].SpanContext().TraceID().String(), entry.Trace)
			require.Equal(t, spans[0].SpanContext().SpanID().String(), entry.SpanID)
			require.Equal(t, "/items/42", entry.HTTPRequest.RequestURL)
			require.NotContains(t, out.String(), "jane.doe")
		})
	}

	t.Run("OptionalInterfaces", func(t *testing.T) {
		t.Parallel()

		logger := &loggingpresets.HTTPGcloud{BaseLogger: &loggingpresets.LogGcloud{Out: io.Discard}}
		requireOptionalInterfaces(t, logger.Logger())
	})
}
