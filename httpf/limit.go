package httpf

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
)

// standardRequests caps each visitor's requests per second on each route.
const standardRequests = 5

// Limits builds rate limits that refuse excess requests with 429 and Retry-After. Counters live in
// the process, so each instance counts on its own.
//
// Mount limits on a router group: chi knows a request's route pattern only once routing matched,
// and limits count per route pattern, so path parameters share one budget.
type Limits struct {
	enabled bool
}

// NewLimits returns the limits, which let every request through when the config disables them.
func NewLimits(config ServerConfig) Limits {
	return Limits{enabled: config.RateLimitsEnabled}
}

// Standard caps each visitor at 5 requests per second on each route.
func (limits Limits) Standard() func(http.Handler) http.Handler {
	return limits.By(standardRequests, time.Second, VisitorKey)
}

// By caps the requests sharing key at limit per window on each route.
func (limits Limits) By(limit int, window time.Duration, key httprate.KeyFunc) func(http.Handler) http.Handler {
	if !limits.enabled {
		return func(next http.Handler) http.Handler { return next }
	}

	return httprate.LimitBy(limit, window, httprate.JoinKeys(key, routeKey))
}

// VisitorKey keys a limit by the visitor IP. An IPv6 visitor counts per /64, the block one client
// usually controls.
func VisitorKey(r *http.Request) (string, error) {
	return "visitor:" + httprate.CanonicalizeIP(middleware.GetClientIP(r.Context())), nil
}

func routeKey(r *http.Request) (string, error) {
	return r.Method + " " + chi.RouteContext(r.Context()).RoutePattern(), nil
}
