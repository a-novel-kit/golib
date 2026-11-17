package httpf_test

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/httpf"
	"github.com/a-novel-kit/golib/internal/tlstest"
	"github.com/a-novel-kit/golib/servertls"
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

		go func() { requestErrors <- getHTTP(t.Context(), server.Addr) }()

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
		case err := <-requestErrors:
			require.Error(t, err)
		case <-time.After(serverTestTimeout):
			require.FailNow(t, "HTTP request did not finish after forced shutdown")
		}
	})
}

// getHTTP sends one GET request to address, then closes the response it gets.
func getHTTP(ctx context.Context, address string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address, nil)
	if err != nil {
		return err
	}

	response, err := http.DefaultClient.Do(request)
	if response != nil {
		err = errors.Join(err, response.Body.Close())
	}

	return err
}

// freeHTTPAddress returns a loopback address no listener holds at the time of the call.
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

// takenHTTPAddress returns a loopback address whose port another listener holds until the test
// ends, on every interface, so the server fails to bind it.
func takenHTTPAddress(t *testing.T) string {
	t.Helper()

	listenerConfig := &net.ListenConfig{}

	listener, err := listenerConfig.Listen(t.Context(), "tcp", ":0")
	if err != nil {
		panic(err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	return "127.0.0.1:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

// httpPort returns the port of address.
func httpPort(t *testing.T, address string) int {
	t.Helper()

	_, port, err := net.SplitHostPort(address)
	if err != nil {
		panic(err)
	}

	number, err := strconv.Atoi(port)
	if err != nil {
		panic(err)
	}

	return number
}

// getStatus sends a GET to url through client, and returns the status of the answer.
func getStatus(t *testing.T, client *http.Client, url string) int {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		panic(err)
	}

	response, err := client.Do(request)
	require.NoError(t, err)

	_ = response.Body.Close()

	return response.StatusCode
}

// waitForHTTPServer returns once address accepts connections.
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

func TestListenAndServe(t *testing.T) {
	t.Parallel()

	authority := tlstest.NewAuthority(t)
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: authority.Pool, MinVersion: tls.VersionTLS13}},
		Timeout:   serverTestTimeout,
	}

	testCases := []struct {
		name string

		tls bool
		// keyPair alters the key pair files the server loads.
		keyPair func(files servertls.Files) servertls.Files
		// plaintextAddress returns the address of the plaintext port, or none.
		plaintextAddress func(t *testing.T) string

		expectErr bool
	}{
		{
			name: "Success",
		},
		{
			name: "Success/TLS",

			tls: true,
		},
		{
			name: "Success/TLSBesidePlaintext",

			tls:              true,
			plaintextAddress: freeHTTPAddress,
		},
		{
			name: "Error/KeyPair",

			tls: true,
			keyPair: func(files servertls.Files) servertls.Files {
				files.KeyFile += ".missing"

				return files
			},

			expectErr: true,
		},
		{
			// The TLS port serves, and stops once its plaintext port fails to bind.
			name: "Error/PlaintextPortTaken",

			tls:              true,
			plaintextAddress: takenHTTPAddress,

			expectErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			address := freeHTTPAddress(t)
			config := httpf.ServerConfigDefault
			config.Port = httpPort(t, address)
			config.Timeouts.Shutdown = time.Second

			if testCase.tls {
				config.TLS, _ = authority.WriteKeyPair(t, t.TempDir())
			}

			if testCase.keyPair != nil {
				config.TLS = testCase.keyPair(config.TLS)
			}

			var plaintextAddress string
			if testCase.plaintextAddress != nil {
				plaintextAddress = testCase.plaintextAddress(t)
				config.PlaintextPort = httpPort(t, plaintextAddress)
			}

			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)

			served := make(chan error, 1)
			go func() { served <- httpf.ListenAndServe(ctx, config, http.NotFoundHandler()) }()

			if testCase.expectErr {
				select {
				case err := <-served:
					require.Error(t, err)
				case <-time.After(serverTestTimeout):
					require.FailNow(t, "the server did not stop after its failure")
				}

				return
			}

			waitForHTTPServer(t, address)

			if testCase.tls {
				require.Equal(t, http.StatusNotFound, getStatus(t, client, "https://"+address))
				// The TLS port answers a plaintext request only to refuse it.
				require.Equal(t, http.StatusBadRequest, getStatus(t, client, "http://"+address))
			} else {
				require.Equal(t, http.StatusNotFound, getStatus(t, client, "http://"+address))
			}

			if plaintextAddress != "" {
				waitForHTTPServer(t, plaintextAddress)
				require.Equal(t, http.StatusNotFound, getStatus(t, client, "http://"+plaintextAddress))
			}

			cancel()

			select {
			case err := <-served:
				require.NoError(t, err)
			case <-time.After(serverTestTimeout):
				require.FailNow(t, "the server did not stop after cancellation")
			}
		})
	}
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	type contextKey struct{}

	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, "process"))
	config := httpf.ServerConfigDefault
	config.Port = 9090

	server := httpf.NewServer(ctx, config, http.NotFoundHandler())

	require.Equal(t, ":9090", server.Addr)
	require.Equal(t, config.Timeouts.Read, server.ReadTimeout)
	require.Equal(t, config.Timeouts.ReadHeader, server.ReadHeaderTimeout)
	require.Equal(t, config.Timeouts.Write, server.WriteTimeout)
	require.Equal(t, config.Timeouts.Idle, server.IdleTimeout)

	// Shutdown begins: requests keep the process values, and run on within the drain.
	cancel()

	base := server.BaseContext(&net.TCPListener{})
	require.Equal(t, "process", base.Value(contextKey{}))
	require.NoError(t, base.Err())
}
