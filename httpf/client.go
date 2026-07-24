package httpf

import (
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Standard-library transport defaults, restated because the transport is built field by field.
const (
	transportDialTimeout           = 30 * time.Second
	transportDialKeepAlive         = 30 * time.Second
	transportIdleConnTimeout       = 90 * time.Second
	transportTLSHandshakeTimeout   = 10 * time.Second
	transportExpectContinueTimeout = 1 * time.Second
)

// ClientOptions sizes the connection pool of the client [NewClient] returns.
type ClientOptions struct {
	// MaxIdleConns caps the idle connections kept across every host.
	MaxIdleConns int

	// MaxIdleConnsPerHost caps the idle connections kept for a single host. Set it to at least the
	// number of concurrent requests made to that host: the standard library's default is two, and
	// every call past the limit pays a fresh TLS handshake.
	MaxIdleConnsPerHost int
}

// NewTransport builds the transport [NewClient] runs on. Use it to read those settings back, or to
// wrap the transport before building a client.
func NewTransport(options ClientOptions) *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   transportDialTimeout,
			KeepAlive: transportDialKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          options.MaxIdleConns,
		MaxIdleConnsPerHost:   options.MaxIdleConnsPerHost,
		IdleConnTimeout:       transportIdleConnTimeout,
		TLSHandshakeTimeout:   transportTLSHandshakeTimeout,
		ExpectContinueTimeout: transportExpectContinueTimeout,

		// Zero: a server that computes its answer before replying sends no headers until it is
		// done, so any value here kills the long calls and spares the short ones. The caller's
		// context deadline is the bound instead.
		ResponseHeaderTimeout: 0,
	}
}

// NewClient builds the client a service makes its outbound calls through, traced with otelhttp so
// each request opens a span under the caller's and carries the trace context onward.
//
// Build one per process and inject it: sharing it is what makes the connection pool useful.
func NewClient(options ClientOptions) *http.Client {
	return &http.Client{
		Transport: otelhttp.NewTransport(NewTransport(options)),

		// Zero: this bounds the whole exchange, so any value truncates a long response mid-body.
		Timeout: 0,
	}
}
