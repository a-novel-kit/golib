package httpf_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/a-novel-kit/golib/httpf"
)

// Every case here goes through a real server, because httptest.ResponseRecorder cannot
// show the defect: its Header() stays live after WriteHeader, so a discarded
// Content-Type still reads back as set. Only a response that has crossed a socket
// reports what a client would actually receive.

type response struct {
	status      int
	contentType string
	body        string
}

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

	span := noop.Span{}

	testCases := []struct {
		name string

		status int
		data   any

		expectBody string
	}{
		{
			name:       "OK",
			status:     http.StatusOK,
			data:       map[string]string{"hello": "world"},
			expectBody: `{"hello":"world"}`,
		},
		{
			// The case the previous signature could not express. Both live 201 handlers
			// declare content: application/json for it in their openapi.yaml.
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
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := call(t, func(w http.ResponseWriter, r *http.Request) {
				httpf.SendJSONStatus(r.Context(), w, span, testCase.status, testCase.data)
			})

			require.Equal(t, testCase.status, got.status)
			require.Equal(t, "application/json", got.contentType)
			require.JSONEq(t, testCase.expectBody, got.body)
		})
	}
}

func TestSendJSONKeepsTheJSONContentType(t *testing.T) {
	t.Parallel()

	// The deprecated signature still answers 200 with a JSON content type, so the 14
	// call sites that never set a status are unaffected by the change.
	got := call(t, func(w http.ResponseWriter, r *http.Request) {
		httpf.SendJSON(r.Context(), w, noop.Span{}, map[string]int{"n": 1})
	})

	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, "application/json", got.contentType)
	require.JSONEq(t, `{"n":1}`, got.body)
}

// Pins the behaviour the new signature exists to make unreachable: net/http freezes the
// header set at WriteHeader, so a Content-Type written afterwards never leaves the
// process and the client is told text/plain.
//
// This is what both live 201 handlers do today, and what SendJSON's own doc used to
// prescribe.
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

// The recorder is why no existing test caught this.
func TestResponseRecorderCannotObserveTheDiscard(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	recorder.WriteHeader(http.StatusCreated)
	recorder.Header().Set("Content-Type", "application/json")

	// A naive assertion against the recorder passes, while a real client is served
	// text/plain — see the test above.
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"),
		"the recorder reports the header a real response would have dropped")
}
