package httpf

import (
	"context"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"

	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/downtime"
	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

// NewRouter returns a router with the standard middleware, outermost first: panic recovery, the
// visitor IP, the request timeout, the body limit, tracing, the report of faults on the request's
// span, CORS, request logs, the planned downtime and the standard rate limit. The liveness path
// answers during a downtime and is never rate-limited. A route under [Streaming] answers past the
// request timeout.
//
// The request's span takes the name of the route chi matched, such as "GET /v2/session/{id}", and
// the route pattern as its http.route. A request that reaches no route keeps its method alone, such
// as one the rate limit refuses. Under a subrouter, an unknown path takes the subrouter's pattern,
// such as "GET /v2/*". A path never names a span.
//
// A nil logger writes no request log, for a server whose spans record each request.
func NewRouter(
	config ServerConfig, tracing otel.Config, logger logging.HTTPConfig, downtimeStart *time.Time,
) *chi.Mux {
	router := chi.NewRouter()

	router.Use(middleware.Recoverer)
	router.Use(middleware.ClientIPFromRemoteAddr)

	if config.ClientIPHeader != "" {
		// The proxy's header names the visitor; a request without it keeps the peer address.
		router.Use(middleware.ClientIPFromHeader(config.ClientIPHeader))
	}

	router.Use(requestTimeout(config.Timeouts.Request))
	router.Use(middleware.RequestSize(config.MaxRequestSize))
	router.Use(tracing.HTTPHandler())
	router.Use(recordFaults)
	router.Use(cors.Handler(corsOptions(config.Cors)))

	if logger != nil {
		router.Use(logger.Logger())
	}

	router.Use(downtime.Middleware(downtimeStart, config.Liveness))

	if config.RateLimitsEnabled {
		router.Use(standardLimit(router, config))
	}

	return router
}

// serverSpanKey holds the span tracing opened for a request, on which [HandleError] names the type of
// a server fault.
type serverSpanKey struct{}

// recordFaults keeps the request's server span in its context, for [HandleError]. It records a panic
// of a later handler on that span, then passes it on to [middleware.Recoverer], which answers it. It
// runs inside tracing, so the server span exists. It names that span after the request's route once
// the handlers return, even through a panic.
func recordFaults(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		defer capturePanic(span)
		defer nameSpan(r.Context(), span)

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), serverSpanKey{}, span)))
	})
}

// nameSpan names span after the route chi matched for the request of ctx, and sets the route pattern
// as its http.route, as the OpenTelemetry HTTP server conventions do. A request chi routed nowhere
// keeps the name tracing gave it.
func nameSpan(ctx context.Context, span trace.Span) {
	pattern := chi.RouteContext(ctx).RoutePattern()
	if pattern == "" {
		return
	}

	span.SetName(route(ctx))
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
// [NewRouter]. Elsewhere, it returns the span of ctx.
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

// NewServer returns a server for handler on the configured port, with the configured timeouts.
//
// Every request context carries the values of ctx, but not its cancellation. ctx is usually the
// process context that [Serve] drains the server under. A request in flight when shutdown begins
// then finishes within the drain. [EndOnShutdown] ends such a request at shutdown instead.
func NewServer(ctx context.Context, config ServerConfig, handler http.Handler) *http.Server {
	base := context.WithoutCancel(ctx)

	return &http.Server{
		Addr:              ":" + strconv.Itoa(config.Port),
		Handler:           handler,
		ReadTimeout:       config.Timeouts.Read,
		ReadHeaderTimeout: config.Timeouts.ReadHeader,
		WriteTimeout:      config.Timeouts.Write,
		IdleTimeout:       config.Timeouts.Idle,
		BaseContext:       func(net.Listener) context.Context { return base },
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

// ListenAndServe serves handler on the configured port until ctx is canceled, then drains active
// requests within the configured shutdown timeout. See [Serve].
func ListenAndServe(ctx context.Context, config ServerConfig, handler http.Handler) error {
	return Serve(ctx, NewServer(ctx, config, handler), config.Timeouts.Shutdown)
}
