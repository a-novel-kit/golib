package httpf

import (
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	transportDialTimeout           = 30 * time.Second
	transportDialKeepAlive         = 30 * time.Second
	transportIdleConnTimeout       = 90 * time.Second
	transportTLSHandshakeTimeout   = 10 * time.Second
	transportExpectContinueTimeout = 1 * time.Second
)

// PoolOptions sizes the connection pool [NewPoolClient] and [NewPoolTransport] build.
type PoolOptions struct {
	// MaxIdleConns caps the idle connections kept across every host.
	MaxIdleConns int

	// MaxIdleConnsPerHost caps the idle connections kept for a single host. Set it to at least the
	// number of concurrent requests made to that host: the standard library keeps two, and every
	// call past that pays a fresh TLS handshake.
	MaxIdleConnsPerHost int
}

// NewPoolTransport returns the transport [NewPoolClient] runs on, untraced. Use it to read the pool
// settings back, or to wrap the transport before building a client.
func NewPoolTransport(options PoolOptions) *http.Transport {
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

		// A value here cuts off a response that is slow to start.
		ResponseHeaderTimeout: 0,
	}
}

// NewPoolClient returns an HTTP client that adds three things to http.DefaultClient: a connection
// pool the caller sizes, otelhttp tracing that opens a span per request and carries the trace
// context to the far end, and no timeout, so a response that takes minutes to arrive survives.
//
// Build one per process and inject it: sharing it is what makes the pool useful.
func NewPoolClient(options PoolOptions) *http.Client {
	return &http.Client{
		Transport: otelhttp.NewTransport(NewPoolTransport(options)),
		Timeout:   0,
	}
}
