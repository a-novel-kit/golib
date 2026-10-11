package httpf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
)

// maxBodyKeyLength caps the value [BodyKeyRate] counts, read before the handler validates it.
const maxBodyKeyLength = 1024

// ErrInvalidBodyKey is returned by a [BodyKeyRate] key for a value too long to count.
var ErrInvalidBodyKey = errors.New("invalid rate limit key")

// Limits builds rate limits that refuse excess requests with 429 and Retry-After. Counters live in
// the process, so each instance counts on its own.
//
// Mount limits on a router group: chi knows a request's route pattern only once routing matched,
// and limits count per route pattern, so path parameters share one budget.
type Limits struct {
	enabled  bool
	standard Rate
}

// NewLimits returns the limits, which let every request through when the config disables them.
func NewLimits(config ServerConfig) Limits {
	return Limits{enabled: config.RateLimitsEnabled, standard: config.RateLimit}
}

// Standard caps each visitor at the configured rate on each route, 5 requests per second by
// default.
func (limits Limits) Standard() func(http.Handler) http.Handler {
	return limits.By(limits.standard, VisitorKey)
}

// By caps the requests sharing key at rate on each route.
func (limits Limits) By(rate Rate, key httprate.KeyFunc) func(http.Handler) http.Handler {
	if !limits.enabled {
		return func(next http.Handler) http.Handler { return next }
	}

	return httprate.LimitBy(
		rate.Requests, rate.Window, httprate.JoinKeys(key, routeKey), httprate.WithErrorHandler(refuseKey),
	)
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

// VisitorKey keys a limit by the visitor IP. An IPv6 visitor counts per /64, the block one client
// usually controls.
func VisitorKey(r *http.Request) (string, error) {
	return "visitor:" + httprate.CanonicalizeIP(middleware.GetClientIP(r.Context())), nil
}

// BodyKeyRate keys a rate limit by a top-level field of the JSON body, so many callers cannot gang
// up on one target such as a mailbox. A string counts trimmed and case-folded, its escapes decoded so
// they cannot spell one target many ways; another value counts as written. It puts the body back
// for the handler. A body it cannot read or decode shares the empty value's budget until the handler
// refuses it; a value over 1 KiB is refused with 400, since nothing has validated it yet.
func BodyKeyRate(field string) httprate.KeyFunc {
	return func(r *http.Request) (string, error) {
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
}

// refuseKey answers 400 to a request whose key cannot be computed, such as an oversized body key.
func refuseKey(w http.ResponseWriter, _ *http.Request, _ error) {
	http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
}

func routeKey(r *http.Request) (string, error) {
	return r.Method + " " + chi.RouteContext(r.Context()).RoutePattern(), nil
}
