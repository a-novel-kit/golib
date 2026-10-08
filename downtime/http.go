package downtime

import (
	"net/http"
	"time"
)

// Tag marks a refused HTTP request in its problem body, under "tags" as
// [github.com/a-novel-kit/golib/httpf.HandleError] writes them.
const Tag = "downtime"

// problem is the RFC 9457 body of every refused request.
const problem = `{"type":"about:blank","title":"Service Unavailable","status":503,"tags":{"` + Tag + `":true}}`

// Middleware refuses every request once start has passed, except paths starting with one of
// open, such as the liveness route. A nil start returns next unchanged.
func Middleware(start *time.Time, open ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if start == nil {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !Started(start, time.Now()) || isOpen(r.URL.Path, open) {
				next.ServeHTTP(w, r)

				return
			}

			Respond(w)
		})
	}
}

// Respond writes the 503 Service Unavailable answer to a request refused for a planned downtime,
// with an RFC 9457 problem body tagged [Tag]. A handler uses it when a dependency it needs is in
// a planned downtime.
func Respond(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusServiceUnavailable)

	// A failed write means the client is gone.
	_, _ = w.Write([]byte(problem))
}
