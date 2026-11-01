package httpf

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Serve listens on server.Addr until ctx is canceled or the server stops. Cancellation drains
// active requests within shutdownTimeout, then closes remaining connections. A forced shutdown
// returns an error wrapping [context.DeadlineExceeded]. A server with a TLSConfig serves TLS, with
// the certificate its configuration presents.
//
// [http.ErrServerClosed] is a normal result. A serving error that races with shutdown is joined
// with the shutdown error so callers can inspect both with [errors.Is].
//
// A command can connect process signals directly:
//
//	processCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
//	defer stop()
//	if err := httpf.Serve(processCtx, server, 30*time.Second); err != nil {
//		log.Printf("run HTTP server: %v", err)
//	}
func Serve(ctx context.Context, server *http.Server, shutdownTimeout time.Duration) error {
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

// serveAll runs [Serve] for each server, and stops them all once ctx is canceled or one of them
// stops.
func serveAll(ctx context.Context, servers []*http.Server, shutdownTimeout time.Duration) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	serveErrors := make(chan error, len(servers))
	for _, server := range servers {
		go func() {
			serveErrors <- Serve(ctx, server, shutdownTimeout)

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
