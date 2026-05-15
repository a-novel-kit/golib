package logging

import (
	"net/http"
)

// HTTPConfig is implemented by anything that produces an HTTP middleware
// chain emitting structured access logs.
type HTTPConfig interface {
	Logger() func(http.Handler) http.Handler
}

// HttpConfig is the legacy spelling of HTTPConfig.
//
// Deprecated: use HTTPConfig. The renamed alias matches the project's
// acronym-casing convention (`HTTP`, not `Http`); behaviour is unchanged.
type HttpConfig = HTTPConfig
