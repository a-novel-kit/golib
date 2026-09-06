package httpf_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/httpf"
)

const serverTestTimeout = 5 * time.Second

func TestServe(t *testing.T) {
	t.Parallel()

	t.Run("Success/ProcessCancellation", func(t *testing.T) {
		t.Parallel()

		address := freeHTTPAddress(t)
		ctx, cancel := context.WithCancel(t.Context())
		server := &http.Server{
			Addr:              address,
			Handler:           http.NotFoundHandler(),
			ReadHeaderTimeout: time.Second,
		}

		serveErrors := make(chan error, 1)
		go func() { serveErrors <- httpf.Serve(ctx, server, time.Second) }()

		waitForHTTPServer(t, address)
		cancel()

		select {
		case err := <-serveErrors:
			require.NoError(t, err)
		case <-time.After(serverTestTimeout):
			require.FailNow(t, "HTTP server did not stop after process cancellation")
		}
	})

	t.Run("Error/BindFailure", func(t *testing.T) {
		t.Parallel()

		listenerConfig := &net.ListenConfig{}
		listener, err := listenerConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, listener.Close()) })

		server := &http.Server{
			Addr:              listener.Addr().String(),
			Handler:           http.NotFoundHandler(),
			ReadHeaderTimeout: time.Second,
		}
		err = httpf.Serve(t.Context(), server, time.Second)

		require.Error(t, err)
		require.Contains(t, err.Error(), "serve HTTP")
	})

	t.Run("Error/ForcedShutdown", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		started := make(chan struct{})
		release := make(chan struct{})

		var releaseOnce sync.Once

		t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

		server := &http.Server{
			Addr:              freeHTTPAddress(t),
			ReadHeaderTimeout: time.Second,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				close(started)
				<-release
				w.WriteHeader(http.StatusNoContent)
			}),
		}

		serveErrors := make(chan error, 1)
		go func() { serveErrors <- httpf.Serve(ctx, server, 50*time.Millisecond) }()

		requestErrors := make(chan error, 1)

		waitForHTTPServer(t, server.Addr)

		go func() {
			request, requestErr := http.NewRequestWithContext(
				t.Context(), http.MethodGet, "http://"+server.Addr, nil,
			)
			if requestErr != nil {
				requestErrors <- requestErr

				return
			}

			response, requestErr := http.DefaultClient.Do(request)
			if response != nil {
				requestErr = errors.Join(requestErr, response.Body.Close())
			}

			requestErrors <- requestErr
		}()

		select {
		case <-started:
		case <-time.After(serverTestTimeout):
			require.FailNow(t, "HTTP handler did not start")
		}

		cancel()

		select {
		case err := <-serveErrors:
			require.ErrorIs(t, err, context.DeadlineExceeded)
		case <-time.After(serverTestTimeout):
			require.FailNow(t, "HTTP server did not stop within its shutdown budget")
		}

		releaseOnce.Do(func() { close(release) })

		select {
		case <-requestErrors:
		case <-time.After(serverTestTimeout):
			require.FailNow(t, "HTTP request did not finish after forced shutdown")
		}
	})
}

func freeHTTPAddress(t *testing.T) string {
	t.Helper()

	listenerConfig := &net.ListenConfig{}

	listener, err := listenerConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	address := listener.Addr().String()

	err = listener.Close()
	if err != nil {
		panic(err)
	}

	return address
}

func waitForHTTPServer(t *testing.T, address string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), serverTestTimeout)
	defer cancel()

	dialer := &net.Dialer{}

	for {
		connection, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			err = connection.Close()
			if err != nil {
				panic(err)
			}

			return
		}

		select {
		case <-ctx.Done():
			panic(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
