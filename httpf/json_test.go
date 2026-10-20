package httpf_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/a-novel-kit/golib/httpf"
	"github.com/a-novel-kit/golib/otel"
)

// response is what a client receives from a handler.
type response struct {
	status      int
	contentType string
	body        string
}

// call serves handler on a real server and returns what a client receives. The Header() of an
// httptest.ResponseRecorder stays live after WriteHeader, so a discarded Content-Type still reads
// back as set there. Only a response that has crossed a socket shows what a client receives.
func call(t *testing.T, handler http.HandlerFunc) response {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	res, err := server.Client().Do(req)
	require.NoError(t, err)

	t.Cleanup(func() { _ = res.Body.Close() })

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	return response{
		status:      res.StatusCode,
		contentType: res.Header.Get("Content-Type"),
		body:        string(body),
	}
}

func TestSendJSONStatus(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		status   int
		data     any
		priorErr error

		expectBody            string
		expectSpanStatus      codes.Code
		expectSpanDescription string
	}{
		{
			name:       "OK",
			status:     http.StatusOK,
			data:       map[string]string{"hello": "world"},
			expectBody: `{"hello":"world"}`,
		},
		{
			// A status other than 200 still answers application/json.
			name:       "Created",
			status:     http.StatusCreated,
			data:       map[string]string{"id": "abc"},
			expectBody: `{"id":"abc"}`,
		},
		{
			name:       "Accepted",
			status:     http.StatusAccepted,
			data:       []string{"queued"},
			expectBody: `["queued"]`,
		},

		{
			// A client error status is an answer, not a fault.
			name:       "ErrorStatus/BadRequest",
			status:     http.StatusBadRequest,
			data:       map[string]string{"status": "invalid"},
			expectBody: `{"status":"invalid"}`,
		},
		{
			// The handler reports a fault's cause; the status alone carries none.
			name:       "ErrorStatus/ServiceUnavailable",
			status:     http.StatusServiceUnavailable,
			data:       map[string]string{"status": "down"},
			expectBody: `{"status":"down"}`,
		},
		{
			name:                  "ErrorStatus/PreviouslyReportedDependency",
			status:                http.StatusServiceUnavailable,
			data:                  map[string]string{"status": "down"},
			priorErr:              errors.New("foo"),
			expectBody:            `{"status":"down"}`,
			expectSpanStatus:      codes.Error,
			expectSpanDescription: "foo",
		},
		{
			name:                  "Error/Encoding",
			status:                http.StatusOK,
			data:                  make(chan int),
			expectSpanStatus:      codes.Error,
			expectSpanDescription: "unsupported type",
		},
		{
			name:                  "Error/ResponseWrite",
			status:                http.StatusNoContent,
			data:                  map[string]string{"status": "up"},
			expectSpanStatus:      codes.Error,
			expectSpanDescription: "does not allow body",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

			_, span := provider.Tracer("httpf-test").Start(t.Context(), "response")
			if testCase.priorErr != nil {
				_ = otel.ReportError(span, testCase.priorErr)
			}

			got := call(t, func(w http.ResponseWriter, r *http.Request) {
				defer span.End()

				httpf.SendJSONStatus(r.Context(), w, span, testCase.status, testCase.data)
			})

			require.Equal(t, testCase.status, got.status)
			require.Equal(t, "application/json", got.contentType)

			if testCase.expectBody == "" {
				require.Empty(t, got.body)
			} else {
				require.JSONEq(t, testCase.expectBody, got.body)
			}

			spans := recorder.Ended()
			require.Len(t, spans, 1)
			require.Equal(t, testCase.expectSpanStatus, spans[0].Status().Code)
			require.Empty(t, spans[0].Events())

			require.Contains(t, spans[0].Status().Description, testCase.expectSpanDescription)
		})
	}
}

// net/http freezes the headers at WriteHeader, so a Content-Type written afterwards never
// leaves the process and the client is told text/plain. SendJSONStatus takes the status
// so that its callers never write in this order.
func TestWritingTheStatusFirstDiscardsTheContentType(t *testing.T) {
	t.Parallel()

	got := call(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "abc"})
	})

	require.Equal(t, http.StatusCreated, got.status)
	require.NotEqual(t, "application/json", got.contentType,
		"if this ever passes, net/http changed and the whole rationale needs revisiting")
	require.Contains(t, got.contentType, "text/plain")
}

// The recorder reports the header a real response drops, so tests of this order need a server.
func TestResponseRecorderCannotObserveTheDiscard(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	recorder.WriteHeader(http.StatusCreated)
	recorder.Header().Set("Content-Type", "application/json")

	// An assertion against the recorder passes, while a real client is served text/plain.
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"),
		"the recorder reports the header a real response would have dropped")
}
