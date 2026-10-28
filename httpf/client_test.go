package httpf_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/propagation"

	"github.com/a-novel-kit/golib/httpf"
	"github.com/a-novel-kit/golib/servicetoken"
)

const (
	// slowHeaderDelay outlasts any response-header timeout a hardening pass would introduce.
	slowHeaderDelay = 250 * time.Millisecond

	clientTraceID      = "0102030405060708090a0b0c0d0e0f10"
	inboundTraceHeader = "00-" + clientTraceID + "-0102030405060708-01"
)

func TestNewPoolTransport(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		options httpf.PoolOptions
	}{
		{
			name: "Success/Sized",

			options: httpf.PoolOptions{MaxIdleConns: 100, MaxIdleConnsPerHost: 4},
		},
		{
			name: "Success/Zeroed",

			options: httpf.PoolOptions{},
		},
	}

	standard := http.DefaultTransport.(*http.Transport)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			transport := httpf.NewPoolTransport(testCase.options)

			require.Equal(t, testCase.options.MaxIdleConns, transport.MaxIdleConns)
			require.Equal(t, testCase.options.MaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)

			// Guards the zero written out in NewPoolTransport.
			require.Zero(t, transport.ResponseHeaderTimeout)

			// The rest comes from the standard transport.
			require.NotNil(t, transport.Proxy)
			require.NotNil(t, transport.DialContext)
			require.Equal(t, standard.ForceAttemptHTTP2, transport.ForceAttemptHTTP2)
			require.Equal(t, standard.IdleConnTimeout, transport.IdleConnTimeout)
			require.Equal(t, standard.TLSHandshakeTimeout, transport.TLSHandshakeTimeout)
			require.Equal(t, standard.ExpectContinueTimeout, transport.ExpectContinueTimeout)
		})
	}

	t.Run("Success/Independent", func(t *testing.T) {
		t.Parallel()

		options := httpf.PoolOptions{MaxIdleConns: 7, MaxIdleConnsPerHost: 3}

		first := httpf.NewPoolTransport(options)
		second := httpf.NewPoolTransport(options)

		first.MaxIdleConnsPerHost = 5
		first.TLSHandshakeTimeout = time.Minute

		// Neither the other transport nor the standard one sees a change made to one transport.
		require.NotSame(t, first, second)
		require.Equal(t, options.MaxIdleConnsPerHost, second.MaxIdleConnsPerHost)
		require.Equal(t, standard.TLSHandshakeTimeout, second.TLSHandshakeTimeout)
		require.NotEqual(t, options.MaxIdleConns, standard.MaxIdleConns)
		require.NotEqual(t, time.Minute, standard.TLSHandshakeTimeout)
	})

	t.Run("Success/NegotiatesHTTP2", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		server.EnableHTTP2 = true
		server.StartTLS()
		t.Cleanup(server.Close)

		transport := httpf.NewPoolTransport(httpf.PoolOptions{})
		t.Cleanup(transport.CloseIdleConnections)

		// The test server's certificate is trusted only through its own client configuration.
		transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig

		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
		require.NoError(t, err)

		response, err := (&http.Client{Transport: transport}).Do(request)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())

		require.Equal(t, 2, response.ProtoMajor)
	})
}

//nolint:paralleltest // It replaces http.DefaultTransport, which every other test reads.
func TestNewPoolTransportReplacedDefault(t *testing.T) {
	original := http.DefaultTransport

	t.Cleanup(func() { http.DefaultTransport = original })

	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("replaced default transport")
	})

	transport := httpf.NewPoolTransport(httpf.PoolOptions{MaxIdleConns: 7, MaxIdleConnsPerHost: 3})

	require.Equal(t, 7, transport.MaxIdleConns)
	require.Equal(t, 3, transport.MaxIdleConnsPerHost)
	require.Nil(t, transport.Proxy)
}

// roundTripperFunc implements http.RoundTripper with a function.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestNewPoolClient(t *testing.T) {
	t.Parallel()

	t.Run("Success/NoOverallTimeout", func(t *testing.T) {
		t.Parallel()

		// Guards the zero written out in NewPoolClient.
		require.Zero(t, httpf.NewPoolClient(httpf.PoolOptions{}).Timeout)
	})

	t.Run("Success/ReusesConnections", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		client := httpf.NewPoolClient(httpf.PoolOptions{MaxIdleConns: 100, MaxIdleConnsPerHost: 4})

		var reused []bool

		clientTrace := &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) { reused = append(reused, info.Reused) },
		}

		for range 3 {
			ctx := httptrace.WithClientTrace(t.Context(), clientTrace)

			request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			require.NoError(t, err)

			response, err := client.Do(request)
			require.NoError(t, err)

			// Draining and closing returns the connection to the idle pool, so this is part of
			// the assertion.
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, errors.Join(err, response.Body.Close()))
		}

		require.Equal(t, []bool{false, true, true}, reused)
	})

	t.Run("Success/SlowResponseHeaders", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(slowHeaderDelay)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
		require.NoError(t, err)

		response, err := httpf.NewPoolClient(httpf.PoolOptions{}).Do(request)
		require.NoError(t, err)

		defer func() { require.NoError(t, response.Body.Close()) }()

		require.Equal(t, http.StatusOK, response.StatusCode)
	})

	t.Run("Success/PropagatesTraceContext", func(t *testing.T) {
		t.Parallel()

		// Buffered so the handler never blocks, and read after Do returns.
		received := make(chan string, 1)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			received <- r.Header.Get("Traceparent")

			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		// An extracted traceparent puts a valid span context on ctx without the tracing SDK.
		carrier := propagation.HeaderCarrier{}
		carrier.Set("traceparent", inboundTraceHeader)
		ctx := propagation.TraceContext{}.Extract(t.Context(), carrier)

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		require.NoError(t, err)

		response, err := httpf.NewPoolClient(httpf.PoolOptions{}).Do(request)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())

		require.Contains(t, <-received, clientTraceID)
	})
}

func TestServiceTokenTransport(t *testing.T) {
	t.Parallel()

	t.Run("Success/ReadsEachRequest", func(t *testing.T) {
		t.Parallel()

		// Buffered so the handler never blocks, and read after Do returns.
		received := make(chan string, 1)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			received <- r.Header.Get(servicetoken.Header)

			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		path := filepath.Join(t.TempDir(), "token")
		client := &http.Client{
			Transport: &httpf.ServiceTokenTransport{ServiceTokenPath: path, Base: http.DefaultTransport},
		}

		// The kubelet replaces the file between the requests.
		for _, token := range []string{"first", "rotated"} {
			require.NoError(t, os.WriteFile(path, []byte(token), 0o600))

			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			require.NoError(t, err)

			response, err := client.Do(request)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())

			require.Equal(t, token, <-received)
			require.Empty(t, request.Header.Get(servicetoken.Header))
		}
	})

	t.Run("Error/MissingToken", func(t *testing.T) {
		t.Parallel()

		transport := &httpf.ServiceTokenTransport{
			ServiceTokenPath: filepath.Join(t.TempDir(), "token"),
			Base:             http.DefaultTransport,
		}

		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:1", nil)
		require.NoError(t, err)

		//nolint:bodyclose // The call never returns a response, so there is no body to close.
		response, err := transport.RoundTrip(request)
		require.ErrorIs(t, err, os.ErrNotExist)
		require.Nil(t, response)
	})
}
