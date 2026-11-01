package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// errRecoveredPanic wraps the panic values [ReportPanic] records.
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

// ForceFlush exports the spans and logs the global providers hold, within ctx, and keeps them
// running. A provider that buffers nothing, such as the default no-op one, has nothing to flush.
func ForceFlush(ctx context.Context) error {
	var errs []error

	for _, provider := range []any{otel.GetTracerProvider(), otel.GetLoggerProvider()} {
		flusher, ok := provider.(interface {
			ForceFlush(ctx context.Context) error
		})
		if ok {
			errs = append(errs, flusher.ForceFlush(ctx))
		}
	}

	return errors.Join(errs...)
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

// ErrorType returns the error.type attribute of err, a server fault, for the span that answers it: a
// server span, or the root span of a job. The span metrics derived from those spans then count faults
// by type.
//
// The attribute names the type of the innermost error err wraps, such as "*pgconn.PgError", since
// the wrappers around it are the same for every fault. Through an error joining several, it follows
// the first.
func ErrorType(err error) attribute.KeyValue {
	return semconv.ErrorTypeKey.String(innermostType(err))
}

// ReportPanic marks span failed by rec, a value recover returned, as [ReportError] does. The span's
// error.type names the type of rec, as [ErrorType] names the type of an error. ReportPanic returns the
// error the span records, whose message holds rec.
func ReportPanic(span trace.Span, rec any) error {
	span.SetAttributes(semconv.ErrorTypeKey.String(innermostType(rec)))

	return ReportError(span, fmt.Errorf("%w: %v", errRecoveredPanic, rec))
}

// RecoverPanic absorbs a panic on the calling goroutine. It records the panic on span through
// [ReportPanic]. Defer it directly, since recover only works from a deferred function.
//
// It also emits one log record at severity ERROR, linked to span. The record's exception.type and
// exception.stacktrace attributes hold the type of the panic and the stack it began on.
//
// Use it on detached goroutines, where an escaped panic ends the process.
func RecoverPanic(ctx context.Context, span trace.Span) {
	rec := recover()
	if rec == nil {
		return
	}

	ctx = trace.ContextWithSpan(ctx, span)

	Logger().ErrorContext(ctx, ReportPanic(span, rec).Error(),
		slog.String(string(semconv.ExceptionTypeKey), innermostType(rec)),
		slog.String(string(semconv.ExceptionStacktraceKey), string(debug.Stack())),
	)
}

// innermostType names the type of the innermost error value wraps, such as "*pgconn.PgError". A
// wrapped error carries no stack trace, so its innermost cause tells faults apart. Through an error
// joining several, it follows the first. A value that wraps nothing, such as a string a panic
// recovered, names its own type.
func innermostType(value any) string {
	for {
		var next error

		switch wrapper := value.(type) {
		case interface{ Unwrap() error }:
			next = wrapper.Unwrap()
		case interface{ Unwrap() []error }:
			if causes := wrapper.Unwrap(); len(causes) > 0 {
				next = causes[0]
			}
		}

		if next == nil {
			return fmt.Sprintf("%T", value)
		}

		value = next
	}
}
