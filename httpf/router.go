package httpf

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/downtime"
	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

// NewMux returns a router with the standard middleware, outermost first: panic recovery, the
// visitor IP, the request timeout, the body limit, tracing, the report of faults on the request's
// span, CORS, request logs, the planned downtime, the standard rate limit and HEAD routing. The
// liveness path answers during a downtime, is never rate-limited, and leaves no span: its requests
// run under a parent span that is not sampled, so the spans their handlers open drop with it. A
// route under [Streaming] answers past the request timeout.
//
// Every GET route answers HEAD too, as RFC 9110 §9.1 requires: the GET handler runs, and net/http
// drops the body it writes. One deviation is deliberate: the Allow header of a 405 lists the methods
// routes register, so a GET route's leaves out HEAD (§15.5.6). Listing it would wrap every response,
// or match the tree again, which mounted subrouters defeat.
//
// The request's span takes the method of the request and the route chi matched, such as
// "GET /v2/session/{id}", and the route pattern as its http.route. A request that reaches no route
// keeps its method alone, such as one the rate limit refuses. Under a subrouter, an unknown path
// takes the subrouter's pattern, such as "GET /v2/*". A path never names a span.
//
// A nil logger writes no request log, for a server whose spans record each request. The router
// traces under the global providers [otel.Init] installs.
func NewMux(config ServerConfig, logger logging.HTTPConfig, downtimeStart *time.Time) *chi.Mux {
	router := chi.NewRouter()

	router.Use(middleware.Recoverer)
	router.Use(middleware.ClientIPFromRemoteAddr)

	if config.ClientIPHeader != "" {
		// The proxy's header names the visitor; a request without it keeps the peer address.
		router.Use(middleware.ClientIPFromHeader(config.ClientIPHeader))
	}

	router.Use(requestTimeout(config.Timeouts.Request))
	router.Use(middleware.RequestSize(config.MaxRequestSize))
	router.Use(traceRequests(config.Liveness))
	router.Use(recordFaults)
	router.Use(cors.Handler(corsOptions(config.Cors)))

	if logger != nil {
		router.Use(logger.Logger())
	}

	router.Use(downtime.Middleware(downtimeStart, config.Liveness))

	if config.RateLimitsEnabled {
		router.Use(standardLimit(router, config))
	}

	router.Use(middleware.GetHead)

	return router
}

// traceRequests opens the server span of each request, under the global tracer provider, except for
// requests to liveness, which run under a parent span that is not sampled.
func traceRequests(liveness string) func(http.Handler) http.Handler {
	traced := otelhttp.NewMiddleware("")

	return func(next http.Handler) http.Handler {
		tracedNext := traced(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == liveness {
				next.ServeHTTP(w, r.WithContext(untracedContext(r.Context())))

				return
			}

			tracedNext.ServeHTTP(w, r)
		})
	}
}

// untracedContext returns ctx under a local parent span that is not sampled.
func untracedContext(ctx context.Context) context.Context {
	var (
		traceID trace.TraceID
		spanID  trace.SpanID
	)

	// crypto/rand.Read never fails.
	_, _ = rand.Read(traceID[:])
	_, _ = rand.Read(spanID[:])

	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))
}

// serverSpanKey holds the span tracing opened for a request, on which [HandleError] sets a server
// fault's error.type.
type serverSpanKey struct{}

// recordFaults keeps the request's server span in its context, for [HandleError]. It records a panic
// of a later handler on that span, then passes it on to [middleware.Recoverer], which answers it. It
// runs inside tracing, so the server span exists. It names that span after the request's route once
// the handlers return, even through a panic.
func recordFaults(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		defer capturePanic(span)
		defer nameSpan(r, span)

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), serverSpanKey{}, span)))
	})
}

// nameSpan names span after the method of r and the route chi matched for it, and sets the route
// pattern as its http.route, as the OpenTelemetry HTTP server conventions do. The method is the
// request's, so a HEAD that a GET route answers keeps its own. A request chi routed nowhere keeps
// the name tracing gave it.
func nameSpan(r *http.Request, span trace.Span) {
	pattern := chi.RouteContext(r.Context()).RoutePattern()
	if pattern == "" {
		return
	}

	span.SetName(r.Method + " " + pattern)
	span.SetAttributes(semconv.HTTPRoute(pattern))
}

// capturePanic records a panic on the calling goroutine on span, then panics again with its value.
// Defer it directly, since recover only works from a deferred function. A handler aborts its response
// with an [http.ErrAbortHandler] panic, which is no fault and stays off the span.
func capturePanic(span trace.Span) {
	rec := recover()
	if rec == nil {
		return
	}

	if rec != http.ErrAbortHandler { //nolint:errorlint // The abort is a panic value, never wrapped.
		_ = otel.ReportPanic(span, rec)
	}

	panic(rec)
}

// serverSpan returns the span tracing opened for the request of ctx, under a router from
// [NewMux]. Elsewhere, it returns the span of ctx.
func serverSpan(ctx context.Context) trace.Span {
	span, ok := ctx.Value(serverSpanKey{}).(trace.Span)
	if !ok {
		return trace.SpanFromContext(ctx)
	}

	return span
}

// corsOptions translates config for the CORS middleware. Browsers hide Retry-After from scripts
// unless it is exposed, and every 429 sends it.
func corsOptions(config CorsConfig) cors.Options {
	return cors.Options{
		AllowedOrigins:   config.AllowedOrigins,
		AllowedHeaders:   config.AllowedHeaders,
		AllowedMethods:   config.AllowedMethods,
		AllowCredentials: config.AllowCredentials,
		ExposedHeaders:   []string{"Retry-After"},
		MaxAge:           config.MaxAge,
	}
}

// standardLimit caps each visitor at the configured rate on each route. Routing has not run yet,
// so it looks the route pattern up itself: path parameters then share their route's budget.
func standardLimit(router *chi.Mux, config ServerConfig) func(http.Handler) http.Handler {
	patternKey := func(r *http.Request) (string, error) {
		return r.Method + " " + router.Find(chi.NewRouteContext(), r.Method, r.URL.Path), nil
	}

	limit := httprate.LimitBy(
		config.RateLimit.Requests, config.RateLimit.Window,
		httprate.JoinKeys(VisitorKey, patternKey), httprate.WithErrorHandler(refuseKey),
	)

	return func(next http.Handler) http.Handler {
		limited := limit(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == config.Liveness {
				next.ServeHTTP(w, r)

				return
			}

			limited.ServeHTTP(w, r)
		})
	}
}

// EndOnShutdown ends the context of a request once ctx is done. Pass as ctx the process context the
// server drains under. Mount it on a long request that keeps taking on work, such as a batch. The
// request then stops at shutdown and answers within the drain. Other requests keep their context
// until they finish.
//
//nolint:contextcheck // A request's context derives from the request; ctx only signals its end.
func EndOnShutdown(ctx context.Context) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCtx, cancel := context.WithCancel(r.Context())
			defer cancel()

			stop := context.AfterFunc(ctx, cancel)
			defer stop()

			next.ServeHTTP(w, r.WithContext(requestCtx))
		})
	}
}

// SendTelemetry sends the spans and logs of a request before its answer completes, within timeout.
// Wrap the whole router with it, outside its tracing, so the request's own span leaves too. It suits
// a server no consumer waits on, billed per request: its platform withdraws the CPU once it answers,
// so telemetry left to the background would wait for the next request.
func SendTelemetry(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)

			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), timeout)
			defer cancel()

			err := otel.ForceFlush(ctx)
			if err != nil {
				log.Println("send telemetry: " + err.Error())
			}
		})
	}
}

// requestTimeoutKey holds the [requestTimeoutState] of a request, which [Streaming] lifts.
type requestTimeoutKey struct{}

// requestTimeoutState is the request timeout of one request.
type requestTimeoutState struct {
	// afterDisconnect calls f once the client disconnects, as [context.AfterFunc] does.
	afterDisconnect func(f func()) (stop func() bool)
	// lifted is set by [Streaming], whose own limit then bounds the request.
	lifted bool
}

// requestTimeout ends a request's context once timeout passes, and answers 504 Gateway Timeout when
// the handler returns past it. A route under [Streaming] lifts it.
func requestTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			untimed := r.Context()
			state := &requestTimeoutState{
				afterDisconnect: func(f func()) func() bool { return context.AfterFunc(untimed, f) },
			}

			ctx, cancel := context.WithTimeout(context.WithValue(r.Context(), requestTimeoutKey{}, state), timeout)
			defer cancel()

			next.ServeHTTP(w, r.WithContext(ctx))

			if !state.lifted && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				w.WriteHeader(http.StatusGatewayTimeout)
			}
		})
	}
}
