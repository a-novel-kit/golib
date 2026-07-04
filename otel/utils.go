package otel

import (
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// AppName is the instrumentation scope name stamped on every tracer and logger
// created through this package. Set it once at startup with SetAppName.
var AppName string

// SetAppName sets the instrumentation scope name used by Tracer and Logger.
func SetAppName(name string) {
	AppName = name
}

// Tracer returns a tracer scoped to AppName from the global tracer provider.
func Tracer(options ...trace.TracerOption) trace.Tracer {
	return otel.GetTracerProvider().Tracer(AppName, options...)
}

// Logger returns an slog logger scoped to AppName that writes through the global
// OpenTelemetry logger provider.
func Logger(options ...otelslog.Option) *slog.Logger {
	return otelslog.NewLogger(AppName, options...)
}

// ReportError records err on the span and marks the span failed, then returns err
// unchanged so a handler can write `return otel.ReportError(span, err)`.
func ReportError(span trace.Span, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	return err
}

// ReportSuccess marks the span successful and returns resp unchanged, for use in a
// span's final return statement.
func ReportSuccess[Resp any](span trace.Span, resp Resp) Resp {
	span.SetStatus(codes.Ok, "")

	return resp
}

// ReportSuccessNoContent marks the span successful when there is no value to return.
func ReportSuccessNoContent(span trace.Span) {
	span.SetStatus(codes.Ok, "")
}
