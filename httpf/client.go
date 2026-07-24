package httpf

import (
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Connection settings that are not worth a per-deployment knob. They restate the standard
// library's own transport defaults, which this package cannot inherit: the transport is built
// field by field rather than cloned from http.DefaultTransport, so that a caller forbidding the
// process-global transport is not made to reach for it here.
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

	// MaxIdleConnsPerHost caps the idle connections kept for a single host, and should be at least
	// the number of requests the caller makes to that host at once. The standard library's default
	// is two, and http.DefaultTransport does not raise it, so every concurrent call past the second
	// against the same host opens a fresh connection and pays a fresh TLS handshake for it — on the
	// latency-critical path, on every call, for the life of the process.
	MaxIdleConnsPerHost int
}

// NewTransport builds the transport [NewClient] runs on. Prefer that constructor; this one is
// exported so a caller can read the settings below back off the returned value, or wrap it.
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

		// Zero on purpose, against the advice of most HTTP hardening guides, which recommend
		// something around twenty seconds. A server that computes its answer before replying — a
		// model generating text, a report being assembled — sends no response headers until it is
		// done, so any value here kills exactly the long calls and leaves the short ones alone.
		// That failure only appears under the slowest inputs, which is to say it passes CI and
		// fails in production.
		//
		// The caller's context deadline is the bound instead: it is the only one that can give a
		// two-second metadata call and a two-minute generation different budgets on one shared
		// client. Written out rather than left off, so the zero reads as a decision.
		ResponseHeaderTimeout: 0,
	}
}

// NewClient builds the client a service makes its outbound calls through, traced with otelhttp so
// each request opens a span under the caller's and carries the trace context to the far end.
//
// Build one per process and inject it. The point of sharing it is the connection pool
// [ClientOptions] sizes: a client built per call pools nothing.
func NewClient(options ClientOptions) *http.Client {
	return &http.Client{
		Transport: otelhttp.NewTransport(NewTransport(options)),

		// Zero for the same reason ResponseHeaderTimeout is, and more bluntly: this one bounds the
		// whole exchange, body included, so any value truncates a long response mid-stream.
		// Deadlines come from the caller's context.
		Timeout: 0,
	}
}
