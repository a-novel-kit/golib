package logging

import (
	"net/http"
)

// HTTPConfig is implemented by anything that produces an HTTP middleware
// chain emitting structured access logs.
type HTTPConfig interface {
	Logger() func(http.Handler) http.Handler
}
