package httpf_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/propagation"

	"github.com/a-novel-kit/golib/httpf"
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
			name: "Success",

			options: httpf.PoolOptions{MaxIdleConns: 100, MaxIdleConnsPerHost: 4},
		},
		{
			name: "Success/Zeroed",

			options: httpf.PoolOptions{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			transport := httpf.NewPoolTransport(testCase.options)

			require.Equal(t, testCase.options.MaxIdleConns, transport.MaxIdleConns)
			require.Equal(t, testCase.options.MaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)

			// Guards the zero written out in NewPoolTransport.
			require.Zero(t, transport.ResponseHeaderTimeout)
		})
	}
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

		for range 3 {
			ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
				GotConn: func(info httptrace.GotConnInfo) {
					reused = append(reused, info.Reused)
				},
			})

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
