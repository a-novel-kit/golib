package logging

import (
	"net/http"
)

// HTTPConfig produces the middleware that logs every HTTP request. The presets subpackage
// implements it for a terminal. Under an OpenTelemetry backend, the server span records each request,
// and a service installs no HTTPConfig.
type HTTPConfig interface {
	// Logger returns a middleware that wraps a handler and records one
	// access-log entry per request, at a level chosen from the response status.
	Logger() func(http.Handler) http.Handler
}
