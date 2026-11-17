package worker

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

// Run runs fn once, as the root span named name of a job, such as "job.RotateKeys". A failure, a
// failure to reach a dependency included, is reported on that span with its type in error.type, as
// the root span of a job must name it, and returned for the job's main to exit on.
//
// The span ends before Run returns, so the main can flush the telemetry before it exits.
func Run(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	ctx, span := otel.Tracer().Start(ctx, name, trace.WithNewRoot())
	defer span.End()

	err := fn(ctx)
	if err != nil {
		err = otel.ReportError(span, err)
		span.SetAttributes(otel.ErrorType(err))

		return err
	}

	return nil
}
