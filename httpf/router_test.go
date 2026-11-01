package httpf_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	gootel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/a-novel-kit/golib/httpf"
	"github.com/a-novel-kit/golib/logging"
	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	"github.com/a-novel-kit/golib/otel"
	otelpresets "github.com/a-novel-kit/golib/otel/presets"
)

func TestNewRouter(t *testing.T) {
	t.Parallel()

	started := time.Now().Add(-time.Hour)

	testCases := []struct {
		name string

		clientIPHeader string
		downtimeStart  *time.Time
		rateLimited    bool
		// withoutRequestLog builds the router with a nil logger.
		withoutRequestLog bool

		method  string
		paths   []string
		headers map[string]string
		body    string

		// The expectations apply to the response to the last path.
		expectStatus  int
		expectBody    string
		expectHeaders map[string]string
		// expectSpanName, when set, is the name of the request's server span, and expectRoute its
		// http.route, empty when absent.
		expectSpanName string
		expectRoute    string
	}{
		{
			name: "Success/VisitorFromPeer",

			method: http.MethodGet,
			paths:  []string{"/visitor"},

			expectStatus:   http.StatusOK,
			expectBody:     "192.0.2.1",
			expectSpanName: "GET /visitor",
			expectRoute:    "/visitor",
		},
		{
			// A path parameter must not reach the span name.
			name: "Success/PathParameters",

			method: http.MethodGet,
			paths:  []string{"/items/42"},

			expectStatus:   http.StatusOK,
			expectSpanName: "GET /items/{id}",
			expectRoute:    "/items/{id}",
		},
		{
			name: "Success/Subrouter",

			method: http.MethodGet,
			paths:  []string{"/v2/items/42"},

			expectStatus:   http.StatusOK,
			expectSpanName: "GET /v2/items/{id}",
			expectRoute:    "/v2/items/{id}",
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
			name: "Success/WithoutRequestLog",

			withoutRequestLog: true,

			method: http.MethodGet,
			paths:  []string{"/visitor"},

			expectStatus: http.StatusOK,
			expectBody:   "192.0.2.1",
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
			// The limit refuses the request before routing.
			name: "Error/RateLimit",

			rateLimited: true,

			method: http.MethodGet,
			paths:  []string{"/visitor", "/visitor", "/visitor"},

			expectStatus:   http.StatusTooManyRequests,
			expectHeaders:  map[string]string{"Retry-After": "60"},
			expectSpanName: "GET",
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
			// The raw path of an unknown route must not reach the span name.
			name: "Error/NotFound",

			method: http.MethodGet,
			paths:  []string{"/missing/42"},

			expectStatus:   http.StatusNotFound,
			expectSpanName: "GET",
		},
		{
			// Under a subrouter, an unknown path takes the subrouter's pattern.
			name: "Error/NotFound/Subrouter",

			method: http.MethodGet,
			paths:  []string{"/v2/missing/42"},

			expectStatus:   http.StatusNotFound,
			expectSpanName: "GET /v2/*",
			expectRoute:    "/v2/*",
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

			var requestLog logging.HTTPConfig = &loggingpresets.HTTPLocal{
				BaseLogger: &loggingpresets.LogLocal{Out: io.Discard},
			}
			if testCase.withoutRequestLog {
				requestLog = nil
			}

			recorder := tracetest.NewSpanRecorder()
			tracing := &tracedConfig{provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))}

			router := httpf.NewRouter(config, tracing, requestLog, testCase.downtimeStart)
			ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
			router.Get("/ping", ok)
			router.Get("/items/{id}", ok)
			router.Route("/v2", func(api chi.Router) { api.Get("/items/{id}", ok) })
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

			if testCase.expectSpanName != "" {
				spans := recorder.Ended()
				span := spans[len(spans)-1]
				attributes := attribute.NewSet(span.Attributes()...)
				route, _ := attributes.Value("http.route")

				require.Equal(t, testCase.expectSpanName, span.Name())
				require.Equal(t, testCase.expectRoute, route.AsString())
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

// tracedConfig is a telemetry backend that exports nothing but traces each request on provider.
type tracedConfig struct {
	otelpresets.Disabled

	provider trace.TracerProvider
}

func (config *tracedConfig) HTTPHandler() func(http.Handler) http.Handler {
	return otelhttp.NewMiddleware("", otelhttp.WithTracerProvider(config.provider))
}

// panicHandler panics with a value that holds caller input.
func panicHandler(http.ResponseWriter, *http.Request) {
	panic("secret")
}

// abortHandler aborts its response, as a proxy does once the client leaves.
func abortHandler(http.ResponseWriter, *http.Request) {
	panic(http.ErrAbortHandler)
}

// newFaultRouter returns a router from [httpf.NewRouter] serving panicHandler on /panic and
// abortHandler on /abort, with the recorder its spans end in.
func newFaultRouter(t *testing.T) (*chi.Mux, *tracetest.SpanRecorder) {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

	router := httpf.NewRouter(
		httpf.ServerConfigDefault,
		&tracedConfig{provider: provider},
		&loggingpresets.HTTPLocal{BaseLogger: &loggingpresets.LogLocal{Out: io.Discard}},
		nil,
	)
	router.Get("/panic", panicHandler)
	router.Get("/abort", abortHandler)

	return router, recorder
}

func TestNewRouterFaults(t *testing.T) {
	t.Parallel()

	t.Run("Error/Panic", func(t *testing.T) {
		t.Parallel()

		router, recorder := newFaultRouter(t)
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/panic", nil)
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusInternalServerError, response.Code)

		// The recoverer answers outside tracing, so the server span holds the fault itself, under its
		// route.
		spans := recorder.Ended()
		require.Equal(t, "GET /panic", spans[len(spans)-1].Name())
		require.Equal(t, codes.Error, spans[len(spans)-1].Status().Code)
		require.Contains(t, spans[len(spans)-1].Attributes(), attribute.String("error.type", "string"))
	})

	t.Run("Success/Abort", func(t *testing.T) {
		t.Parallel()

		router, recorder := newFaultRouter(t)
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/abort", nil)

		// The recoverer passes an abort on to the server, which closes the connection.
		require.PanicsWithValue(t, http.ErrAbortHandler, func() {
			router.ServeHTTP(httptest.NewRecorder(), request)
		})

		spans := recorder.Ended()
		require.Equal(t, codes.Unset, spans[len(spans)-1].Status().Code)
	})
}

//nolint:paralleltest // Swaps the process-wide tracer provider.
func TestSendTelemetry(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Hour)))

	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

	gootel.SetTracerProvider(provider)
	t.Cleanup(func() { gootel.SetTracerProvider(noop.NewTracerProvider()) })

	handler := httpf.SendTelemetry(time.Second)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, span := otel.Tracer().Start(r.Context(), "sweep")
		span.End()
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/sweep", nil))

	require.Len(t, exporter.GetSpans(), 1)
}
