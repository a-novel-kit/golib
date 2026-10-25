package loggingpresets_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
)

func TestHTTPLocal(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		// statuses are passed to WriteHeader in order, before the body is written.
		statuses []int
		body     string

		expectLine string
		// expectBody reports whether the line is followed by the response body.
		expectBody bool
	}{
		{
			name:       "Success",
			statuses:   []int{http.StatusOK},
			body:       "done",
			expectLine: "✓ GET /items/42",
		},
		{
			// net/http sends the first final status and drops the second.
			name:       "Success/RepeatedWriteHeader",
			statuses:   []int{http.StatusCreated, http.StatusInternalServerError},
			body:       "created",
			expectLine: "✓ GET /items/42",
		},
		{
			name:       "Error/Client",
			statuses:   []int{http.StatusBadRequest},
			body:       "bad input",
			expectLine: "⚠ GET /items/42",
			expectBody: true,
		},
		{
			name:       "Error/Server",
			statuses:   []int{http.StatusInternalServerError},
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
				for _, status := range testCase.statuses {
					w.WriteHeader(status)
				}

				_, _ = io.WriteString(w, testCase.body)
			}))

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequestWithContext(
				t.Context(), http.MethodGet, "/items/42", nil,
			))

			require.Equal(t, testCase.body, recorder.Body.String())
			require.Contains(t, out.String(), testCase.expectLine)

			if testCase.expectBody {
				require.Contains(t, out.String(), testCase.body)
			} else {
				require.NotContains(t, out.String(), testCase.body)
			}
		})
	}

	t.Run("Error/LongBody", func(t *testing.T) {
		t.Parallel()

		// The log keeps the start of a long failure body. The client still receives all of it.
		body := strings.Repeat("x", 1<<20) + "end"

		out := &bytes.Buffer{}
		logger := &loggingpresets.HTTPLocal{BaseLogger: &loggingpresets.LogLocal{Out: out}}

		handler := logger.Logger()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)

			_, _ = io.WriteString(w, body)
		}))

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequestWithContext(
			t.Context(), http.MethodGet, "/items/42", nil,
		))

		require.Equal(t, body, recorder.Body.String())
		require.Contains(t, out.String(), "xxxx")
		require.NotContains(t, out.String(), "end")
		require.Less(t, out.Len(), len(body))
	})

	t.Run("OptionalInterfaces", func(t *testing.T) {
		t.Parallel()

		logger := &loggingpresets.HTTPLocal{BaseLogger: &loggingpresets.LogLocal{Out: io.Discard}}
		requireOptionalInterfaces(t, logger.Logger())
	})
}
