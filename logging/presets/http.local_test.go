package loggingpresets_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
)

func TestHTTPLocal(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		status int
		body   string

		expectLine string
		// expectBody reports whether the line is followed by the response body.
		expectBody bool
	}{
		{
			name:       "Success",
			status:     http.StatusOK,
			body:       "done",
			expectLine: "✓ GET /items/42",
		},
		{
			name:       "Error/Client",
			status:     http.StatusBadRequest,
			body:       "bad input",
			expectLine: "⚠ GET /items/42",
			expectBody: true,
		},
		{
			name:       "Error/Server",
			status:     http.StatusInternalServerError,
			body:       "server failed",
			expectLine: "🗙 GET /items/42",
			expectBody: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			out := &bytes.Buffer{}
			logger := &loggingpresets.HTTPLocal{BaseLogger: &loggingpresets.LogLocal{Out: out}}

			handler := logger.Logger()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = io.WriteString(w, testCase.body)
			}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(
				t.Context(), http.MethodGet, "/items/42", nil,
			))

			require.Contains(t, out.String(), testCase.expectLine)

			if testCase.expectBody {
				require.Contains(t, out.String(), testCase.body)
			} else {
				require.NotContains(t, out.String(), testCase.body)
			}
		})
	}
}
