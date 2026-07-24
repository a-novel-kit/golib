// Package worker holds the loops a service runs in the background, beside the request path.
package worker

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"

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
// Give it the boot context: a request context dies at the server's own timeout.
func Poll(
	ctx context.Context,
	logger logging.Log,
	name string,
	interval, stagger time.Duration,
	fn func(context.Context) (bool, error),
) {
	ctx, span := otel.Tracer().Start(ctx, "worker.Poll")
	defer span.End()

	span.SetAttributes(attribute.String("poll.name", name))

	if !wait(ctx.Done(), stagger) {
		return
	}

	// The condition is what stops the drain path below from running fn again on a cancelled
	// context.
	for ctx.Err() == nil {
		worked, err := runOnce(ctx, fn)

		switch {
		case err != nil:
			logger.Err(ctx, fmt.Sprintf("poll %s: %v", name, err))
		case worked:
			logger.Info(ctx, fmt.Sprintf("poll %s: ran a unit of work", name))

			continue
		}

		if !wait(ctx.Done(), interval) {
			return
		}
	}
}

// runOnce runs fn under its own span, so one tick's latency and outcome stay visible.
func runOnce(ctx context.Context, fn func(context.Context) (bool, error)) (bool, error) {
	ctx, span := otel.Tracer().Start(ctx, "worker.Poll(runOnce)")
	defer span.End()

	worked, err := fn(ctx)
	if err != nil {
		return false, otel.ReportError(span, err)
	}

	span.SetAttributes(attribute.Bool("poll.worked", worked))

	return otel.ReportSuccess(span, worked), nil
}

// wait waits out duration, reporting whether it finished rather than the loop being stopped. Zero
// or less returns immediately, still honoring an already-stopped loop. It takes the cancellation
// channel because waiting is not an operation worth a span.
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
