package httpf_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/httpf"
	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	otelpresets "github.com/a-novel-kit/golib/otel/presets"
)

func TestNewRouter(t *testing.T) {
	t.Parallel()

	started := time.Now().Add(-time.Hour)

	testCases := []struct {
		name string

		clientIPHeader string
		downtimeStart  *time.Time

		method  string
		path    string
		headers map[string]string
		body    string

		expectStatus  int
		expectBody    string
		expectHeaders map[string]string
	}{
		{
			name: "Success/VisitorFromPeer",

			method: http.MethodGet,
			path:   "/visitor",

			expectStatus: http.StatusOK,
			expectBody:   "192.0.2.1",
		},
		{
			name: "Success/VisitorFromTrustedHeader",

			clientIPHeader: "X-Client-Ip",

			method:  http.MethodGet,
			path:    "/visitor",
			headers: map[string]string{"X-Client-Ip": "203.0.113.7"},

			expectStatus: http.StatusOK,
			expectBody:   "203.0.113.7",
		},
		{
			// Without a trusted header configured, anyone could claim any address.
			name: "Success/VisitorIgnoresUntrustedHeader",

			method:  http.MethodGet,
			path:    "/visitor",
			headers: map[string]string{"X-Client-Ip": "203.0.113.7"},

			expectStatus: http.StatusOK,
			expectBody:   "192.0.2.1",
		},
		{
			name: "Success/CorsExposesRetryAfter",

			method:  http.MethodGet,
			path:    "/visitor",
			headers: map[string]string{"Origin": "https://studio.test"},

			expectStatus:  http.StatusOK,
			expectHeaders: map[string]string{"Access-Control-Expose-Headers": "Retry-After"},
		},
		{
			name: "Success/DowntimeLiveness",

			downtimeStart: &started,

			method: http.MethodGet,
			path:   "/ping",

			expectStatus: http.StatusOK,
		},
		{
			name: "Error/BodyTooLarge",

			method: http.MethodPost,
			path:   "/body",
			body:   strings.Repeat("a", 17),

			expectStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name: "Error/Downtime",

			downtimeStart: &started,

			method: http.MethodGet,
			path:   "/visitor",

			expectStatus: http.StatusServiceUnavailable,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := httpf.ServerConfigDefault
			config.ClientIPHeader = testCase.clientIPHeader
			config.MaxRequestSize = 16

			router := httpf.NewRouter(config, httpf.RouterOptions{
				Otel:          &otelpresets.Disabled{},
				Logger:        &loggingpresets.HTTPLocal{BaseLogger: &loggingpresets.LogLocal{Out: io.Discard}},
				DowntimeStart: testCase.downtimeStart,
				Liveness:      "/ping",
				Methods:       []string{http.MethodGet, http.MethodPost},
			})
			router.Get("/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			router.Get("/visitor", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(middleware.GetClientIP(r.Context())))
			})
			router.Post("/body", func(w http.ResponseWriter, r *http.Request) {
				_, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
				}
			})

			request := httptest.NewRequestWithContext(
				t.Context(), testCase.method, testCase.path, strings.NewReader(testCase.body),
			)
			request.RemoteAddr = "192.0.2.1:41234"

			for name, value := range testCase.headers {
				request.Header.Set(name, value)
			}

			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			require.Equal(t, testCase.expectStatus, response.Code)

			if testCase.expectBody != "" {
				require.Equal(t, testCase.expectBody, response.Body.String())
			}

			for name, value := range testCase.expectHeaders {
				require.Equal(t, value, response.Header().Get(name))
			}
		})
	}
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	type contextKey struct{}

	ctx := context.WithValue(t.Context(), contextKey{}, "process")
	config := httpf.ServerConfigDefault
	config.Port = 9090

	server := httpf.NewServer(ctx, config, http.NotFoundHandler())

	require.Equal(t, ":9090", server.Addr)
	require.Equal(t, config.Timeouts.Read, server.ReadTimeout)
	require.Equal(t, config.Timeouts.ReadHeader, server.ReadHeaderTimeout)
	require.Equal(t, config.Timeouts.Write, server.WriteTimeout)
	require.Equal(t, config.Timeouts.Idle, server.IdleTimeout)
	require.Equal(t, "process", server.BaseContext(&net.TCPListener{}).Value(contextKey{}))
}
