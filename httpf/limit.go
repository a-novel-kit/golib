package httpf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	httprateredis "github.com/go-chi/httprate-redis"
	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"

	"go.opentelemetry.io/otel/attribute"
)

// maxBodyKeyLength caps the value [BodyKeyRate] counts, read before the handler validates it.
const maxBodyKeyLength = 1024

// ErrInvalidBodyKey is returned by a [BodyKeyRate] key for a value too long to count.
var ErrInvalidBodyKey = errors.New("invalid rate limit key")

// Limits builds the rate limits a service adds to [NewRouter]'s standard one, refusing excess
// requests with 429 and Retry-After. Their counters live in the process, so each instance counts on
// its own, unless the config names a [RateLimitStore].
//
// With a store, the instances share each limit's budget. A store that fails a command, or takes
// over 100 ms to answer it, sends the limit back to counting in the instance until the store
// answers again. The span of each request counted that way carries rate_limit.local, set to true.
//
// Mount a limit on a route, with chi's With: it counts per route pattern, so path parameters share
// one budget.
type Limits struct {
	enabled bool
	// store holds the counters every instance shares, nil to keep them in the process.
	store *redis.Client
}

// NewLimits returns the limits, which let every request through when the config disables them. It
// opens no connection, so a store that does not answer yet only starts the limits counting in the
// instance.
func NewLimits(config ServerConfig) Limits {
	limits := Limits{enabled: config.RateLimitsEnabled}
	if config.RateLimitStore.Addr == "" {
		return limits
	}

	limits.store = redis.NewClient(&redis.Options{
		Addr:     config.RateLimitStore.Addr,
		Username: config.RateLimitStore.Username,
		Password: config.RateLimitStore.Password,
		// A limit counts in the instance once a command fails. A slow store fails fast: one dial and no
		// retry, within the timeouts httprate-redis gives its own client.
		DialTimeout:   200 * time.Millisecond,
		DialerRetries: 1,
		ReadTimeout:   100 * time.Millisecond,
		WriteTimeout:  100 * time.Millisecond,
		MaxRetries:    -1,
		// A connection logs in with HELLO alone, so the store's ACL can deny every other connection
		// command.
		DisableIdentity:          true,
		MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled},
	})

	return limits
}

// By caps the requests sharing key at rate on each route.
func (limits Limits) By(rate Rate, key httprate.KeyFunc) func(http.Handler) http.Handler {
	if !limits.enabled {
		return func(next http.Handler) http.Handler { return next }
	}

	perRoute := httprate.JoinKeys(key, routeKey)
	if limits.store != nil {
		return limits.byStore(rate, perRoute)
	}

	return httprate.LimitBy(rate.Requests, rate.Window, perRoute, httprate.WithErrorHandler(refuseKey))
}

// byStore builds a limit of [Limits.By] that counts through the store. Each limit takes a counter of
// its own, which carries its window and its fallback to the instance's counters.
func (limits Limits) byStore(rate Rate, key httprate.KeyFunc) func(http.Handler) http.Handler {
	counter := httprateredis.NewCounter(&httprateredis.Config{Client: limits.store})

	refuseRate := func(w http.ResponseWriter, r *http.Request) {
		recordLocal(r, counter.IsFallbackActivated())
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
	}

	// Every limit counts in one store, so the key names the rate too. Two limits of one route and
	// key, such as 10 per minute and 30 per 10 minutes, then stay apart when their windows start
	// together.
	limit := httprate.LimitBy(
		rate.Requests, rate.Window, httprate.JoinKeys(key, httprate.Key(rate.String())),
		httprate.WithLimitCounter(counter),
		httprate.WithLimitHandler(refuseRate),
		httprate.WithErrorHandler(refuseKey),
	)

	return func(next http.Handler) http.Handler {
		return limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recordLocal(r, counter.IsFallbackActivated())
			next.ServeHTTP(w, r)
		}))
	}
}

// recordLocal marks the span of r as counted in the instance, when local holds.
func recordLocal(r *http.Request, local bool) {
	if local {
		serverSpan(r.Context()).SetAttributes(attribute.Bool("rate_limit.local", true))
	}
}

// LimitConcurrency caps the requests a route handles at once, per instance, answering any beyond
// it with 429 and a one-second Retry-After: a slot frees up as soon as one request returns. Use it
// on a route whose calls are slow or costly. Unlike [Limits], test stacks never turn it off.
func LimitConcurrency(maxConcurrent int) func(http.Handler) http.Handler {
	return middleware.ThrottleWithOpts(middleware.ThrottleOpts{
		Limit:        maxConcurrent,
		RetryAfterFn: func(bool) time.Duration { return time.Second },
	})
}

// LimitConcurrencyBy caps the requests sharing a key that its routes handle at once, per instance,
// answering any beyond it with 429 and a one-second Retry-After. Use it on routes whose requests
// last, such as streams, so one caller cannot hold every connection. Mount one value on several
// routes and they share the cap. A request whose key cannot be computed answers 400.
func LimitConcurrencyBy(maxConcurrent int, key httprate.KeyFunc) func(http.Handler) http.Handler {
	var (
		mu sync.Mutex
		// open counts the requests in flight per key. A key leaves it with its last request.
		open = map[string]int{}
	)

	acquire := func(name string) bool {
		mu.Lock()
		defer mu.Unlock()

		if open[name] >= maxConcurrent {
			return false
		}

		open[name]++

		return true
	}

	release := func(name string) {
		mu.Lock()
		defer mu.Unlock()

		open[name]--
		if open[name] == 0 {
			delete(open, name)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name, err := key(r)
			if err != nil {
				refuseKey(w, r, err)

				return
			}

			if !acquire(name) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)

				return
			}

			defer release(name)

			next.ServeHTTP(w, r)
		})
	}
}

// VisitorKey keys a limit by the visitor IP. An IPv6 visitor counts per /64, the block one client
// usually controls.
func VisitorKey(r *http.Request) (string, error) {
	return "visitor:" + httprate.CanonicalizeIP(middleware.GetClientIP(r.Context())), nil
}

// BodyKeyRate keys a rate limit by a top-level field of the JSON body, so many callers cannot gang
// up on one target such as a mailbox. A string counts trimmed and case-folded, its escapes decoded
// so they cannot spell one target many ways. Another value counts as written. It puts the body back
// for the handler.
//
// A body it cannot read or decode shares the budget of the empty value until the handler refuses
// it. A value over 1 KiB is refused with 400, since nothing has validated it yet.
func BodyKeyRate(field string) httprate.KeyFunc {
	return func(r *http.Request) (string, error) {
		return bodyKey(r, field)
	}
}

// bodyKey reads the key [BodyKeyRate] counts r under, then puts the body back.
func bodyKey(r *http.Request, field string) (string, error) {
	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))

	var fields map[string]json.RawMessage
	if err == nil {
		_ = json.Unmarshal(body, &fields) // A malformed body keeps the empty value.
	}

	raw := fields[field]
	if len(raw) > maxBodyKeyLength {
		return "", fmt.Errorf("%w: %q exceeds %d bytes", ErrInvalidBodyKey, field, maxBodyKeyLength)
	}

	value := string(raw)

	var text string
	if json.Unmarshal(raw, &text) == nil {
		value = text
	}

	return field + ":" + strings.ToLower(strings.TrimSpace(value)), nil
}

// refuseKey answers 400 to a request whose key cannot be computed, such as an oversized body key.
func refuseKey(w http.ResponseWriter, _ *http.Request, _ error) {
	http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
}

// routeKey keys a limit by the method and the route pattern routing matched, so each route counts
// apart.
func routeKey(r *http.Request) (string, error) {
	return r.Method + " " + chi.RouteContext(r.Context()).RoutePattern(), nil
}
