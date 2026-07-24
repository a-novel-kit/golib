// Package worker holds the loops a service runs in the background, beside the request path.
//
// A background loop is boilerplate every service otherwise rewrites: a ticker, a select on the
// cancellation channel, and the handful of decisions around them that are easy to get subtly wrong
// — whether an error stops the loop, whether a busy tick waits out the interval anyway, and whether
// several loops sharing one interval all wake on the same tick.
package worker

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

// Poll runs fn on a fixed interval until ctx is cancelled.
//
// When fn reports it did work, Poll runs it again immediately rather than waiting out the interval,
// so a backlog drains at full speed instead of one item per tick. An error is logged and the loop
// continues: a transient failure reaching a dependency must not take a background loop down for the
// life of the process.
//
// stagger delays the first run only, and is deterministic rather than randomised. Several loops
// sharing one interval de-synchronise without drawing from math/rand, which security linters object
// to and which would make an interleaving impossible to reproduce.
//
// name identifies the loop in its log lines and on its spans. Every message is formatted into the
// message string with no fields, because the two [logging] presets read fields differently — the
// local one as printf operands, the Google Cloud one as structured attributes — and agree only when
// there are none.
//
// Poll's own span covers the life of the loop; each run of fn opens its own beneath it, so
// background work has a trace root of its own rather than none at all.
//
// Give it the boot context, never a request's: a request context dies at the server's own timeout,
// and one taken mid-transaction would hand the loop a transaction to run on.
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

	// The condition, rather than a bare for, is what stops the drain path below from running fn
	// again on a cancelled context.
	for ctx.Err() == nil {
		worked, err := runOnce(ctx, fn)

		switch {
		case err != nil:
			logger.Err(ctx, fmt.Sprintf("poll %s: %v", name, err))
		case worked:
			// Worth a line of its own: it is the difference between a backlog draining and a
			// backlog nobody noticed.
			logger.Info(ctx, fmt.Sprintf("poll %s: ran a unit of work", name))

			continue
		}

		if !wait(ctx.Done(), interval) {
			return
		}
	}
}

// runOnce runs fn under its own span, so one tick's latency and outcome are visible rather than
// folded into the span covering the whole loop.
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

// wait waits out duration, reporting whether it finished rather than the loop being stopped. A
// duration of zero or less returns immediately, still honouring an already-stopped loop.
//
// It takes the cancellation channel rather than the context on purpose: waiting is not an operation
// worth a span of its own, and not taking a context is how a function says so.
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
