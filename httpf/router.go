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
	router.Use(tracing.HttpHandler())
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   config.Cors.AllowedOrigins,
		AllowedHeaders:   config.Cors.AllowedHeaders,
		AllowedMethods:   config.Cors.AllowedMethods,
		AllowCredentials: config.Cors.AllowCredentials,
		// Browsers hide Retry-After from scripts unless it is exposed; every 429 sends it.
		ExposedHeaders: []string{"Retry-After"},
		MaxAge:         config.Cors.MaxAge,
	}))
	router.Use(logger.Logger())
	router.Use(downtime.Middleware(downtimeStart, config.Liveness))

	if config.RateLimitsEnabled {
		router.Use(standardLimit(router, config))
	}

	return router
}

// standardLimit caps each visitor at the configured rate on each route. Routing has not run yet,
// so it looks the route pattern up itself: path parameters then share their route's budget.
func standardLimit(router *chi.Mux, config ServerConfig) func(http.Handler) http.Handler {
	routeKey := func(r *http.Request) (string, error) {
		return r.Method + " " + router.Find(chi.NewRouteContext(), r.Method, r.URL.Path), nil
	}

	limit := httprate.LimitBy(
		config.RateLimit.Requests, config.RateLimit.Window,
		httprate.JoinKeys(VisitorKey, routeKey), httprate.WithErrorHandler(refuseKey),
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
// Every request context carries ctx's values, but not its cancellation: ctx is usually the process
// context [Serve] drains the server under, and a request in flight when shutdown begins finishes
// within the drain rather than being cut at once.
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

// ListenAndServe serves handler on the configured port until ctx is canceled, then drains active
// requests within the configured shutdown timeout. See [Serve].
func ListenAndServe(ctx context.Context, config ServerConfig, handler http.Handler) error {
	return Serve(ctx, NewServer(ctx, config, handler), config.Timeouts.Shutdown)
}
