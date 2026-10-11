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

	"github.com/a-novel-kit/golib/downtime"
	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

// RouterOptions holds what a router needs beyond its [ServerConfig].
type RouterOptions struct {
	// Otel traces every request.
	Otel otel.Config
	// Logger logs every request.
	Logger logging.HTTPConfig
	// DowntimeStart is when a planned downtime starts; nil when none is planned.
	DowntimeStart *time.Time
	// Liveness is the ping path, which still answers during a planned downtime.
	Liveness string
	// Methods lists the methods cross-origin requests may use.
	Methods []string
}

// NewRouter returns a router with the standard middleware, outermost first: panic recovery, the
// visitor IP, the request timeout, the body limit, tracing, CORS, request logs and the planned
// downtime. Mount the liveness route on it directly, and the others on a group behind
// [Limits.Standard].
func NewRouter(config ServerConfig, options RouterOptions) *chi.Mux {
	router := chi.NewRouter()

	router.Use(middleware.Recoverer)
	router.Use(middleware.ClientIPFromRemoteAddr)

	if config.ClientIPHeader != "" {
		// The proxy's header names the visitor; a request without it keeps the peer address.
		router.Use(middleware.ClientIPFromHeader(config.ClientIPHeader))
	}

	router.Use(middleware.Timeout(config.Timeouts.Request))
	router.Use(middleware.RequestSize(config.MaxRequestSize))
	router.Use(options.Otel.HttpHandler())
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   config.Cors.AllowedOrigins,
		AllowedHeaders:   config.Cors.AllowedHeaders,
		AllowCredentials: config.Cors.AllowCredentials,
		AllowedMethods:   options.Methods,
		// Browsers hide Retry-After from scripts unless it is exposed; every 429 sends it.
		ExposedHeaders: []string{"Retry-After"},
		MaxAge:         config.Cors.MaxAge,
	}))
	router.Use(options.Logger.Logger())
	router.Use(downtime.Middleware(options.DowntimeStart, options.Liveness))

	return router
}

// NewServer returns a server for handler on the configured port, with the configured timeouts.
// Every request context derives from ctx.
func NewServer(ctx context.Context, config ServerConfig, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":" + strconv.Itoa(config.Port),
		Handler:           handler,
		ReadTimeout:       config.Timeouts.Read,
		ReadHeaderTimeout: config.Timeouts.ReadHeader,
		WriteTimeout:      config.Timeouts.Write,
		IdleTimeout:       config.Timeouts.Idle,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
}
