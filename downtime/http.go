package downtime

import (
	"encoding/json"
	"net/http"
	"slices"
	"time"
)

// Tag is the key of the window in the problem body of a refused HTTP request, under "tags" as
// [github.com/a-novel-kit/golib/httpf.HandleError] writes them.
const Tag = "downtime"

// problem is the RFC 9457 body of a refused request.
type problem struct {
	Type   string             `json:"type"`
	Title  string             `json:"title"`
	Status int                `json:"status"`
	Tags   map[string]*Window `json:"tags"`
}

// Middleware refuses requests to service while window is in progress, except for paths starting
// with one of open, such as the health and status routes. A nil window, or one that doesn't list
// service, returns next unchanged.
func Middleware(window *Window, service string, open ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if window == nil || !slices.Contains(window.Services, service) {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !window.InProgress(service, time.Now()) || isOpen(r.URL.Path, open) {
				next.ServeHTTP(w, r)

				return
			}

			Respond(w, window)
		})
	}
}

// Respond writes the 503 Service Unavailable answer to a request refused for window: an RFC 9457
// problem body carrying the window under [Tag], and a Retry-After header while its end is ahead.
// A handler uses it when a dependency it needs is in a planned downtime.
func Respond(w http.ResponseWriter, window *Window) {
	body, err := json.Marshal(problem{
		Type:   "about:blank",
		Title:  http.StatusText(http.StatusServiceUnavailable),
		Status: http.StatusServiceUnavailable,
		Tags:   map[string]*Window{Tag: window},
	})

	if window.End.After(time.Now()) {
		w.Header().Set("Retry-After", window.End.UTC().Format(http.TimeFormat))
	}

	// Only a year outside 0-9999 fails to encode; the refusal stands without its body.
	if err != nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)

		return
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusServiceUnavailable)

	// A failed write means the client is gone.
	_, _ = w.Write(body)
}
