package otelpresets

import (
	"context"
	"fmt"
	stdlog "log"
	"os"
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

// The bold ANSI colors of the startup banners: bright green for a local run, bright blue for an
// export to a backend.
const (
	bannerStart  = "\x1b[1;92m"
	bannerExport = "\x1b[1;94m"
)

// printBanner prints the startup banner text to standard output, in color.
func printBanner(color, text string) {
	_, _ = fmt.Fprintln(os.Stdout, color+text+"\x1b[0m")
}
