package downtime_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/downtime"
)

func TestMiddleware(t *testing.T) {
	t.Parallel()

	started := time.Now().Add(-time.Minute)
	scheduled := time.Now().Add(time.Hour)

	testCases := []struct {
		name string

		start *time.Time
		path  string

		expectStatus int
	}{
		{
			name:         "Success/NoDowntime",
			path:         "/v2/claims",
			expectStatus: http.StatusOK,
		},
		{
			name:         "Success/Scheduled",
			start:        &scheduled,
			path:         "/v2/claims",
			expectStatus: http.StatusOK,
		},
		{
			name:         "Success/OpenPath",
			start:        &started,
			path:         "/v2/ping",
			expectStatus: http.StatusOK,
		},
		{
			name:         "Error/Started",
			start:        &started,
			path:         "/v2/claims",
			expectStatus: http.StatusServiceUnavailable,
		},
		{
			name:         "Error/HealthIsNotLiveness",
			start:        &started,
			path:         "/v2/healthcheck",
			expectStatus: http.StatusServiceUnavailable,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			handler := downtime.Middleware(testCase.start, "/v2/ping")(next)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, testCase.path, nil))

			require.Equal(t, testCase.expectStatus, recorder.Code)

			if testCase.expectStatus != http.StatusServiceUnavailable {
				return
			}

			// Clients tell a planned downtime from an outage by the tag.
			require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
			require.JSONEq(t, `{"type": "about:blank", "title": "Service Unavailable", "status": 503,
				"tags": {"downtime": true}}`, recorder.Body.String())
		})
	}
}
