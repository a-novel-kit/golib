package logging

import (
	"net/http"
)

// HTTPConfig produces the middleware that logs every HTTP request. The presets
// subpackage implements it in a local (human-readable) and a Google Cloud
// (structured) variant.
type HTTPConfig interface {
	// Logger returns a middleware that wraps a handler and records one
	// access-log entry per request, at a level chosen from the response status.
	Logger() func(http.Handler) http.Handler
}
