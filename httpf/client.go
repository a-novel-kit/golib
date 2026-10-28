package httpf

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/a-novel-kit/golib/servicetoken"
)

// PoolOptions sizes the connection pool [NewPoolClient] and [NewPoolTransport] build.
type PoolOptions struct {
	// MaxIdleConns caps the idle connections kept across every host.
	MaxIdleConns int

	// MaxIdleConnsPerHost caps the idle connections kept for a single host. Set it to at least the
	// number of concurrent requests to that host. The standard library keeps two, and every call
	// past that pays a fresh TLS handshake.
	MaxIdleConnsPerHost int
}

// NewPoolTransport returns the transport [NewPoolClient] runs on, untraced. Use it to read the pool
// settings back, or to wrap the transport before building a client.
//
// The transport is a clone of [http.DefaultTransport], so its proxy, dial, TLS and HTTP/2 settings
// follow the standard library. When a program has replaced the default with another
// [http.RoundTripper], the transport starts from the zero [http.Transport] instead.
func NewPoolTransport(options PoolOptions) *http.Transport {
	standard, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		standard = &http.Transport{}
	}

	transport := standard.Clone()
	transport.MaxIdleConns = options.MaxIdleConns
	transport.MaxIdleConnsPerHost = options.MaxIdleConnsPerHost

	// A value here cuts off a response that is slow to start.
	transport.ResponseHeaderTimeout = 0

	return transport
}

// NewPoolClient returns an HTTP client for calls to other systems. It differs from
// http.DefaultClient in two ways:
//   - options size its connection pool;
//   - otelhttp opens a span per request and carries the trace context to the far end.
//
// Like http.DefaultClient, it sets no timeout, so a response that takes minutes to arrive survives.
//
// Build one per process and inject it: sharing it is what makes the pool useful.
func NewPoolClient(options PoolOptions) *http.Client {
	return &http.Client{
		Transport: otelhttp.NewTransport(NewPoolTransport(options)),
		Timeout:   0,
	}
}

// ServiceTokenTransport sends the caller's service token with every request it carries, which the
// callee checks with [ServiceTokenMiddleware]. Wrap the transport of a client that calls services of
// the same cluster, and no other: whoever receives the token can replay it against the service it
// was projected for.
type ServiceTokenTransport struct {
	// ServiceTokenPath is the file the kubelet projects the caller's service token into. Each request
	// reads it again, so a rotated token takes effect on the next request.
	ServiceTokenPath string
	// Base carries the requests, such as the transport of a client from [NewPoolClient].
	Base http.RoundTripper
}

func (transport *ServiceTokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	token, err := servicetoken.Read(transport.ServiceTokenPath)
	if err != nil {
		// A transport closes the request body, even when it fails.
		if r.Body != nil {
			_ = r.Body.Close()
		}

		return nil, err
	}

	// A transport leaves the caller's request unchanged.
	authenticated := r.Clone(r.Context())
	authenticated.Header.Set(servicetoken.Header, token)

	return transport.Base.RoundTrip(authenticated)
}
