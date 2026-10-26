package httpf_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/httpf"
	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	otelpresets "github.com/a-novel-kit/golib/otel/presets"
	"github.com/a-novel-kit/golib/sentryf/sentryftest"
)

func TestNewRouter(t *testing.T) {
	t.Parallel()

	started := time.Now().Add(-time.Hour)

	testCases := []struct {
		name string

		clientIPHeader string
		downtimeStart  *time.Time
		rateLimited    bool

		method  string
		paths   []string
		headers map[string]string
		body    string

		// The expectations apply to the response to the last path.
		expectStatus  int
		expectBody    string
		expectHeaders map[string]string
	}{
		{
			name: "Success/VisitorFromPeer",

			method: http.MethodGet,
			paths:  []string{"/visitor"},

			expectStatus: http.StatusOK,
			expectBody:   "192.0.2.1",
		},
		{
			name: "Success/VisitorFromTrustedHeader",

			clientIPHeader: "X-Client-Ip",

			method:  http.MethodGet,
			paths:   []string{"/visitor"},
			headers: map[string]string{"X-Client-Ip": "203.0.113.7"},

			expectStatus: http.StatusOK,
			expectBody:   "203.0.113.7",
		},
		{
			// Without a trusted header configured, anyone could claim any address.
			name: "Success/VisitorIgnoresUntrustedHeader",

			method:  http.MethodGet,
			paths:   []string{"/visitor"},
			headers: map[string]string{"X-Client-Ip": "203.0.113.7"},

			expectStatus: http.StatusOK,
			expectBody:   "192.0.2.1",
		},
		{
			name: "Success/CorsExposesRetryAfter",

			method:  http.MethodGet,
			paths:   []string{"/visitor"},
			headers: map[string]string{"Origin": "https://studio.test"},

			expectStatus:  http.StatusOK,
			expectHeaders: map[string]string{"Access-Control-Expose-Headers": "Retry-After"},
		},
		{
			name: "Success/DowntimeLiveness",

			downtimeStart: &started,

			method: http.MethodGet,
			paths:  []string{"/ping"},

			expectStatus: http.StatusOK,
		},
		{
			// Probes reach the liveness path whatever the budget.
			name: "Success/RateLimit/Liveness",

			rateLimited: true,

			method: http.MethodGet,
			paths:  []string{"/ping", "/ping", "/ping"},

			expectStatus: http.StatusOK,
		},
		{
			name: "Error/RateLimit",

			rateLimited: true,

			method: http.MethodGet,
			paths:  []string{"/visitor", "/visitor", "/visitor"},

			expectStatus:  http.StatusTooManyRequests,
			expectHeaders: map[string]string{"Retry-After": "60"},
		},
		{
			// Path parameters must not open a fresh budget per value.
			name: "Error/RateLimit/PathParameters",

			rateLimited: true,

			method: http.MethodGet,
			paths:  []string{"/items/1", "/items/2", "/items/3"},

			expectStatus: http.StatusTooManyRequests,
		},
		{
			name: "Error/BodyTooLarge",

			method: http.MethodPost,
			paths:  []string{"/body"},
			body:   strings.Repeat("a", 17),

			expectStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name: "Error/Downtime",

			downtimeStart: &started,

			method: http.MethodGet,
			paths:  []string{"/visitor"},

			expectStatus: http.StatusServiceUnavailable,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := httpf.ServerConfigDefault
			config.ClientIPHeader = testCase.clientIPHeader
			config.MaxRequestSize = 16
			config.RateLimitsEnabled = testCase.rateLimited
			config.RateLimit = httpf.Rate{Requests: 2, Window: time.Minute}

			router := httpf.NewRouter(
				config,
				&otelpresets.Disabled{},
				&loggingpresets.HTTPLocal{BaseLogger: &loggingpresets.LogLocal{Out: io.Discard}},
				testCase.downtimeStart,
			)
			ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
			router.Get("/ping", ok)
			router.Get("/items/{id}", ok)
			router.Get("/visitor", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(middleware.GetClientIP(r.Context())))
			})
			router.Post("/body", func(w http.ResponseWriter, r *http.Request) {
				_, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
				}
			})

			var response *httptest.ResponseRecorder

			for _, path := range testCase.paths {
				request := httptest.NewRequestWithContext(
					t.Context(), testCase.method, path, strings.NewReader(testCase.body),
				)
				request.RemoteAddr = "192.0.2.1:41234"

				for name, value := range testCase.headers {
					request.Header.Set(name, value)
				}

				response = httptest.NewRecorder()
				router.ServeHTTP(response, request)
			}

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

func TestListenAndServe(t *testing.T) {
	t.Parallel()

	address := freeHTTPAddress(t)
	_, port, err := net.SplitHostPort(address)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	config := httpf.ServerConfigDefault
	config.Port, err = strconv.Atoi(port)
	require.NoError(t, err)

	config.Timeouts.Shutdown = time.Second

	served := make(chan error, 1)
	go func() { served <- httpf.ListenAndServe(ctx, config, http.NotFoundHandler()) }()

	waitForHTTPServer(t, address)
	cancel()

	select {
	case err := <-served:
		require.NoError(t, err)
	case <-time.After(serverTestTimeout):
		require.FailNow(t, "the server did not stop after cancellation")
	}
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	type contextKey struct{}

	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, "process"))
	config := httpf.ServerConfigDefault
	config.Port = 9090

	server := httpf.NewServer(ctx, config, http.NotFoundHandler())

	require.Equal(t, ":9090", server.Addr)
	require.Equal(t, config.Timeouts.Read, server.ReadTimeout)
	require.Equal(t, config.Timeouts.ReadHeader, server.ReadHeaderTimeout)
	require.Equal(t, config.Timeouts.Write, server.WriteTimeout)
	require.Equal(t, config.Timeouts.Idle, server.IdleTimeout)

	// Shutdown begins: requests keep the process values, and run on within the drain.
	cancel()

	base := server.BaseContext(&net.TCPListener{})
	require.Equal(t, "process", base.Value(contextKey{}))
	require.NoError(t, base.Err())
}

func TestEndOnShutdown(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		// shutdown begins while the request runs.
		shutdown bool

		expectErr error
	}{
		{
			name: "Success/ShutdownEndsTheRequest",

			shutdown: true,

			expectErr: context.Canceled,
		},
		{
			name: "Success/RequestRunsWithoutShutdown",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			type contextKey struct{}

			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)

			var (
				requestErr error
				value      any
			)

			handler := httpf.EndOnShutdown(ctx)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				if testCase.shutdown {
					cancel()

					select {
					case <-r.Context().Done():
					case <-time.After(serverTestTimeout):
					}
				}

				requestErr = r.Context().Err()
				value = r.Context().Value(contextKey{})
			}))

			request := httptest.NewRequestWithContext(
				context.WithValue(t.Context(), contextKey{}, "request"), http.MethodPost, "/", nil,
			)
			handler.ServeHTTP(httptest.NewRecorder(), request)

			require.ErrorIs(t, requestErr, testCase.expectErr)
			require.Equal(t, "request", value)
		})
	}
}

// tracedConfig is a telemetry backend that exports nothing but traces each request on provider,
// continuing the W3C trace context the request carries.
type tracedConfig struct {
	otelpresets.Disabled

	provider trace.TracerProvider
}

func (config *tracedConfig) HTTPHandler() func(http.Handler) http.Handler {
	return otelhttp.NewMiddleware(
		"", otelhttp.WithTracerProvider(config.provider), otelhttp.WithPropagators(propagation.TraceContext{}),
	)
}

// panicHandler panics with a value that holds caller input.
func panicHandler(http.ResponseWriter, *http.Request) {
	panic("secret")
}

// abortHandler aborts its response, as a proxy does once the client leaves.
func abortHandler(http.ResponseWriter, *http.Request) {
	panic(http.ErrAbortHandler)
}

//nolint:paralleltest // Records through the process-wide Sentry reporter.
func TestNewRouterSentry(t *testing.T) {
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"

	provider := sdktrace.NewTracerProvider()

	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

	router := httpf.NewRouter(
		httpf.ServerConfigDefault,
		&tracedConfig{provider: provider},
		&loggingpresets.HTTPLocal{BaseLogger: &loggingpresets.LogLocal{Out: io.Discard}},
		nil,
	)
	router.Get("/panic", panicHandler)
	router.Get("/abort", abortHandler)

	// newRequest returns a request on path that holds data a visitor sends, under the trace traceID.
	newRequest := func(t *testing.T, path string) *http.Request {
		t.Helper()

		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path+"?token=secret", nil)
		request.RemoteAddr = "203.0.113.7:41234"
		request.Header.Set("Traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
		request.Header.Set("Authorization", "Bearer secret")
		request.Header.Set("Cookie", "session=secret")

		return request
	}

	t.Run("Error/Panic", func(t *testing.T) {
		transport := sentryftest.Record(t)
		response := httptest.NewRecorder()

		router.ServeHTTP(response, newRequest(t, "/panic"))

		require.Equal(t, http.StatusInternalServerError, response.Code)

		events := transport.Events()
		require.Len(t, events, 1)

		event := events[0]
		require.Equal(t, "string", event.Exception[0].Type)
		require.Equal(t, sentryftest.Release, event.Release)
		require.Equal(t, traceID, event.Contexts["trace"]["trace_id"])
		require.Nil(t, event.Request)

		// The stack ends where the panic began.
		frames := event.Exception[0].Stacktrace.Frames
		require.Equal(t, "panicHandler", frames[len(frames)-1].Function)

		payload, err := json.Marshal(event)
		require.NoError(t, err)
		require.NotContains(t, string(payload), "secret")
		require.NotContains(t, string(payload), "203.0.113.7")
	})

	t.Run("Success/Abort", func(t *testing.T) {
		transport := sentryftest.Record(t)

		// The recoverer passes an abort on to the server, which closes the connection.
		require.PanicsWithValue(t, http.ErrAbortHandler, func() {
			router.ServeHTTP(httptest.NewRecorder(), newRequest(t, "/abort"))
		})

		require.Empty(t, transport.Events())
	})
}
