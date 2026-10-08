package downtime_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/downtime"
)

func TestMiddleware(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	inProgress := window(now, -time.Minute, time.Hour, "json-keys", "authentication")

	testCases := []struct {
		name string

		window *downtime.Window
		path   string

		expectStatus     int
		expectRetryAfter string
	}{
		{
			name:         "Success/NoWindow",
			path:         "/v2/claims",
			expectStatus: http.StatusOK,
		},
		{
			name:         "Success/Notice",
			window:       window(now, time.Hour, 2*time.Hour, "json-keys"),
			path:         "/v2/claims",
			expectStatus: http.StatusOK,
		},
		{
			name:         "Success/Unlisted",
			window:       window(now, -time.Minute, time.Hour, "authentication"),
			path:         "/v2/claims",
			expectStatus: http.StatusOK,
		},
		{
			name:         "Success/OpenPath",
			window:       inProgress,
			path:         "/v2/healthcheck",
			expectStatus: http.StatusOK,
		},
		{
			name:             "Error/InProgress",
			window:           inProgress,
			path:             "/v2/claims",
			expectStatus:     http.StatusServiceUnavailable,
			expectRetryAfter: inProgress.End.Format(http.TimeFormat),
		},
		{
			// Past its end, a window keeps refusing work but no longer promises a time.
			name:         "Error/PastEnd",
			window:       window(now, -2*time.Hour, -time.Hour, "json-keys"),
			path:         "/v2/claims",
			expectStatus: http.StatusServiceUnavailable,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			handler := downtime.Middleware(testCase.window, "json-keys", "/v2/healthcheck", "/v2/ping")(next)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, testCase.path, nil))

			require.Equal(t, testCase.expectStatus, recorder.Code)
			require.Equal(t, testCase.expectRetryAfter, recorder.Header().Get("Retry-After"))

			if testCase.expectStatus != http.StatusServiceUnavailable {
				return
			}

			// Clients read the window from the problem body.
			var body struct {
				Status int                         `json:"status"`
				Tags   map[string]*downtime.Window `json:"tags"`
			}

			require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, http.StatusServiceUnavailable, body.Status)
			require.Equal(t, testCase.window, body.Tags[downtime.Tag])
		})
	}
}
