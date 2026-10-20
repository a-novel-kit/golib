package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// errRecoveredPanic wraps panic values absorbed by [RecoverPanic].
var errRecoveredPanic = errors.New("recovered panic")

// appName is the instrumentation scope name stamped on every tracer and logger
// created through this package.
var appName string

// SetAppName sets the instrumentation scope name of Tracer and Logger. Call it once at startup.
func SetAppName(name string) {
	appName = name
}

// Tracer returns a tracer from the global tracer provider, scoped to the name given to
// [SetAppName].
func Tracer(options ...trace.TracerOption) trace.Tracer {
	return otel.GetTracerProvider().Tracer(appName, options...)
}

// Logger returns an slog logger that writes through the global OpenTelemetry logger provider,
// scoped to the name given to [SetAppName].
func Logger(options ...otelslog.Option) *slog.Logger {
	return otelslog.NewLogger(appName, options...)
}

// reportedError marks an error that a span already describes.
type reportedError struct {
	error
}

func (err reportedError) Unwrap() error {
	return err.error
}

// ReportError marks the span failed and returns err, so a function can write
// `return otel.ReportError(span, err)`.
//
// The first span to report an error describes it with the error message. Each span the error
// then propagates through takes the Error status alone, since the trace already links it to the
// span holding the detail. The returned error carries that mark through any wrapping: compare it
// with errors.Is, never ==.
func ReportError(span trace.Span, err error) error {
	if errors.As(err, new(reportedError)) {
		span.SetStatus(codes.Error, "")

		return err
	}

	span.SetStatus(codes.Error, err.Error())

	return reportedError{err}
}

// ReportSuccess returns resp. OpenTelemetry leaves the status of a successful operation unset, so
// it records nothing on the span. Return resp directly instead.
func ReportSuccess[Resp any](_ trace.Span, resp Resp) Resp {
	return resp
}

// ReportSuccessNoContent records nothing. OpenTelemetry leaves the status of a successful
// operation unset, so omit the call instead.
func ReportSuccessNoContent(_ trace.Span) {}

// RecoverPanic absorbs a panic on the calling goroutine, recording it on span and logging it
// with a stack trace. Defer it directly, since recover only works from a deferred function.
//
// Use it on detached goroutines, where an escaped panic ends the process.
func RecoverPanic(ctx context.Context, span trace.Span) {
	rec := recover()
	if rec == nil {
		return
	}

	err := fmt.Errorf("%w: %v", errRecoveredPanic, rec)
	Logger().ErrorContext(ctx, ReportError(span, err).Error()+"\n"+string(debug.Stack()))
}
