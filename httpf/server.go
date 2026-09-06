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
// returns an error wrapping [context.DeadlineExceeded].
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
		serveErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		closeErr := server.Close()

		return errors.Join(
			fmt.Errorf("serve HTTP: %w", err),
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

func wrapServerError(operation string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", operation, err)
}
