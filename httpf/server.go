package httpf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/a-novel-kit/golib/servertls"
)

// serve listens on server.Addr until ctx is canceled or the server stops. Cancellation drains
// active requests within shutdownTimeout, then closes remaining connections. A forced shutdown
// returns an error wrapping [context.DeadlineExceeded]. A server with a TLSConfig serves TLS, with
// the certificate its configuration presents.
//
// [http.ErrServerClosed] is a normal result. A serving error that races with shutdown is joined
// with the shutdown error so callers can inspect both with [errors.Is].
func serve(ctx context.Context, server *http.Server, shutdownTimeout time.Duration) error {
	serveErrors := make(chan error, 1)

	go func() {
		if server.TLSConfig == nil {
			serveErrors <- server.ListenAndServe()

			return
		}

		// The configuration presents the certificate, so no file is named here.
		serveErrors <- server.ListenAndServeTLS("", "")
	}()

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		closeErr := server.Close()

		return errors.Join(
			wrapServerError("serve HTTP", err),
			wrapServerError("close HTTP server", closeErr),
		)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	shutdownErr := server.Shutdown(shutdownCtx)

	var closeErr error
	if shutdownErr != nil {
		closeErr = server.Close()
	}

	serveErr := <-serveErrors
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}

	return errors.Join(
		wrapServerError("serve HTTP", serveErr),
		wrapServerError("shut down HTTP server", shutdownErr),
		wrapServerError("close HTTP server", closeErr),
	)
}

// serveAll runs serve for each server, and stops them all once ctx is canceled or one of them
// stops.
func serveAll(ctx context.Context, servers []*http.Server, shutdownTimeout time.Duration) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	serveErrors := make(chan error, len(servers))
	for _, server := range servers {
		go func() {
			serveErrors <- serve(ctx, server, shutdownTimeout)

			cancel()
		}()
	}

	var err error
	for range servers {
		err = errors.Join(err, <-serveErrors)
	}

	return err
}

// wrapServerError prefixes err with the operation that failed, and keeps a nil err nil.
func wrapServerError(operation string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", operation, err)
}

// newServer returns a server for handler on the configured port, with the configured timeouts.
//
// Every request context carries the values of ctx, but not its cancellation. ctx is usually the
// process context that [ListenAndServe] drains the server under. A request in flight when shutdown begins
// then finishes within the drain. [EndOnShutdown] ends such a request at shutdown instead.
func newServer(ctx context.Context, config ServerConfig, handler http.Handler) *http.Server {
	base := context.WithoutCancel(ctx)

	return &http.Server{
		Addr:              ":" + strconv.Itoa(config.Port),
		Handler:           handler,
		ReadTimeout:       config.Timeouts.Read,
		ReadHeaderTimeout: config.Timeouts.ReadHeader,
		WriteTimeout:      config.Timeouts.Write,
		IdleTimeout:       config.Timeouts.Idle,
		BaseContext:       func(net.Listener) context.Context { return base },
	}
}

// ListenAndServe serves handler on the configured port until ctx is canceled, then drains active
// requests within the configured shutdown timeout, then closes remaining connections. A forced
// shutdown returns an error wrapping [context.DeadlineExceeded]. A serving error that races with
// shutdown is joined with the shutdown error, so callers can inspect both with [errors.Is].
//
// A command can connect process signals directly:
//
//	processCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
//	defer stop()
//	if err := httpf.ListenAndServe(processCtx, cfg.Rest, router); err != nil {
//		log.Printf("run HTTP server: %v", err)
//	}
//
// The port serves TLS once config.TLS names a key pair, which [servertls.Config] loads and reloads
// at renewal. A config.PlaintextPort also serves plaintext beside it. The listeners stop together:
// one that fails stops the other.
func ListenAndServe(ctx context.Context, config ServerConfig, handler http.Handler) error {
	servers := []*http.Server{newServer(ctx, config, handler)}

	if config.TLS != (servertls.Files{}) {
		tlsConfig, err := servertls.Config(config.TLS)
		if err != nil {
			return fmt.Errorf("serve HTTPS: %w", err)
		}

		servers[0].TLSConfig = tlsConfig
	}

	if config.PlaintextPort != 0 {
		plaintext := config
		plaintext.Port = config.PlaintextPort
		servers = append(servers, newServer(ctx, plaintext, handler))
	}

	return serveAll(ctx, servers, config.Timeouts.Shutdown)
}
