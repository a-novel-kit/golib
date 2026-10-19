package worker

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

// Poll runs fn on a fixed interval until ctx is cancelled. When fn reports it did work, Poll runs
// it again immediately, so a backlog drains at full speed. An error is logged and the loop
// continues.
//
// stagger delays the first run, so loops sharing an interval de-synchronize. name identifies the
// loop in its log lines and on its spans.
//
// Each run is the root of its own trace, so each reaches the exporter as soon as it ends.
//
// Give it the boot context: a request context dies at the server's own timeout.
func Poll(
	ctx context.Context,
	logger logging.Log,
	name string,
	interval, stagger time.Duration,
	fn func(context.Context) (bool, error),
) {
	if !wait(ctx.Done(), stagger) {
		return
	}

	// The condition is what stops the drain path below from running fn again on a cancelled
	// context.
	for ctx.Err() == nil {
		if runOnce(ctx, logger, name, fn) {
			continue
		}

		if !wait(ctx.Done(), interval) {
			return
		}
	}
}

// runOnce runs fn as the root of its own trace, so one run's latency and outcome stay visible, and
// logs under it. It reports whether fn did work.
func runOnce(
	ctx context.Context, logger logging.Log, name string, fn func(context.Context) (bool, error),
) bool {
	ctx, span := otel.Tracer().Start(
		ctx, "worker.Poll", trace.WithNewRoot(), trace.WithAttributes(attribute.String("poll.name", name)),
	)
	defer span.End()

	worked, err := fn(ctx)
	if err != nil {
		logger.Err(ctx, fmt.Sprintf("poll %s: %v", name, otel.ReportError(span, err)))

		return false
	}

	span.SetAttributes(attribute.Bool("poll.worked", worked))

	if worked {
		logger.Info(ctx, fmt.Sprintf("poll %s: ran a unit of work", name))
	}

	return worked
}

// wait waits out duration, and reports false when done closes first. A duration of zero or less
// returns at once, reporting false for a loop already stopped.
func wait(done <-chan struct{}, duration time.Duration) bool {
	if duration <= 0 {
		select {
		case <-done:
			return false
		default:
			return true
		}
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-done:
		return false
	case <-timer.C:
		return true
	}
}
