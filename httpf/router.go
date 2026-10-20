package httpf

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"

	"github.com/a-novel-kit/golib/downtime"
	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

// NewRouter returns a router with the standard middleware, outermost first: panic recovery, the
// visitor IP, the request timeout, the body limit, tracing, CORS, request logs, the planned
// downtime and the standard rate limit. The liveness path answers during a downtime and is never
// rate-limited.
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

	router.Use(middleware.Timeout(config.Timeouts.Request))
	router.Use(middleware.RequestSize(config.MaxRequestSize))
	router.Use(tracing.HTTPHandler())
	router.Use(cors.Handler(corsOptions(config.Cors)))
	router.Use(logger.Logger())
	router.Use(downtime.Middleware(downtimeStart, config.Liveness))

	if config.RateLimitsEnabled {
		router.Use(standardLimit(router, config))
	}

	return router
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
// Every request context carries the values of ctx but not its cancellation. ctx is usually the
// process context [Serve] drains the server under, so a request in flight when shutdown begins
// finishes within the drain. [EndOnShutdown] ends such a request at shutdown instead.
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

// EndOnShutdown ends a request's context once ctx, the process context the server drains under, is
// done. Mount it on a long request that keeps taking on work, such as a batch, so it stops at
// shutdown and answers within the drain. Other requests keep their context until they finish.
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

// ListenAndServe serves handler on the configured port until ctx is canceled, then drains active
// requests within the configured shutdown timeout. See [Serve].
func ListenAndServe(ctx context.Context, config ServerConfig, handler http.Handler) error {
	return Serve(ctx, NewServer(ctx, config, handler), config.Timeouts.Shutdown)
}
