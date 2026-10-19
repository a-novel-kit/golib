package otelpresets

import (
	"context"
	stdlog "log"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// shutdownProviders drains and stops the providers a preset built, within timeout when it is
// positive. A failed drain drops what was still buffered, so it is logged.
func shutdownProviders(timeout time.Duration, tp *sdktrace.TracerProvider, lp *sdklog.LoggerProvider) {
	ctx := context.Background()

	if timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if tp != nil {
		err := tp.Shutdown(ctx)
		if err != nil {
			stdlog.Printf("failed to shutdown tracer provider: %v\n", err)
		}
	}

	if lp != nil {
		err := lp.Shutdown(ctx)
		if err != nil {
			stdlog.Printf("failed to shutdown logger provider: %v\n", err)
		}
	}
}
