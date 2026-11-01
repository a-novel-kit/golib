package otel_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gootel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/log"
	lognoop "go.opentelemetry.io/otel/log/noop"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/a-novel-kit/golib/otel"
)

// driverError stands for an error type of a dependency, whose message holds caller input.
type driverError struct{}

func (*driverError) Error() string {
	return "password authentication failed for user=secret"
}

// recordingExporter keeps a copy of every log record it exports.
type recordingExporter struct {
	records []sdklog.Record
}

func (exporter *recordingExporter) Export(_ context.Context, records []sdklog.Record) error {
	for _, record := range records {
		exporter.records = append(exporter.records, record.Clone())
	}

	return nil
}

func (exporter *recordingExporter) Shutdown(context.Context) error   { return nil }
func (exporter *recordingExporter) ForceFlush(context.Context) error { return nil }

func TestReportError(t *testing.T) {
	t.Parallel()

	errFoo := errors.New("foo")
	// A child span already describes this one.
	errReported := otel.ReportError(trace.SpanFromContext(context.Background()), errFoo)

	testCases := []struct {
		name string

		err error

		expectDescription string
	}{
		{
			name:              "Origin",
			err:               fmt.Errorf("select row: %w", errFoo),
			expectDescription: "select row: foo",
		},
		{
			name: "Propagated",
			err:  fmt.Errorf("select row: %w", errReported),
		},
		{
			name: "PropagatedUnwrapped",
			err:  errReported,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

			_, span := provider.Tracer("otel-test").Start(t.Context(), "operation")
			err := otel.ReportError(span, testCase.err)

			span.End()

			require.ErrorIs(t, err, errFoo)
			require.Equal(t, testCase.err.Error(), err.Error())

			spans := recorder.Ended()
			require.Len(t, spans, 1)
			require.Equal(t, codes.Error, spans[0].Status().Code)
			require.Equal(t, testCase.expectDescription, spans[0].Status().Description)
			require.Empty(t, spans[0].Events())
		})
	}
}

func TestErrorType(t *testing.T) {
	t.Parallel()

	errFoo := errors.New("foo")
	errReported := otel.ReportError(trace.SpanFromContext(context.Background()), &driverError{})

	testCases := []struct {
		name string

		err error

		expectType string
	}{
		{
			name:       "Success/Wrapped",
			err:        fmt.Errorf("select session: %w", &driverError{}),
			expectType: "*otel_test.driverError",
		},
		{
			// golib's own marks, such as a span's report, wrap the cause.
			name:       "Success/Reported",
			err:        fmt.Errorf("select session: %w", errReported),
			expectType: "*otel_test.driverError",
		},
		{
			name:       "Success/JoinedFollowsFirst",
			err:        errors.Join(fmt.Errorf("select jwk: %w", &driverError{}), errFoo),
			expectType: "*otel_test.driverError",
		},
		{
			name:       "Success/Sentinel",
			err:        fmt.Errorf("rotate keys: %w", errFoo),
			expectType: "*errors.errorString",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, attribute.String("error.type", testCase.expectType), otel.ErrorType(testCase.err))
		})
	}
}

func TestReportPanic(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		rec any

		expectType        string
		expectDescription string
	}{
		{
			name:              "Success/String",
			rec:               "boom",
			expectType:        "string",
			expectDescription: "recovered panic: boom",
		},
		{
			name:              "Success/WrappedError",
			rec:               fmt.Errorf("connect: %w", &driverError{}),
			expectType:        "*otel_test.driverError",
			expectDescription: "recovered panic: connect: password authentication failed for user=secret",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

			_, span := provider.Tracer("otel-test").Start(t.Context(), "operation")
			err := otel.ReportPanic(span, testCase.rec)

			span.End()

			require.EqualError(t, err, testCase.expectDescription)

			spans := recorder.Ended()
			require.Len(t, spans, 1)
			require.Equal(t, codes.Error, spans[0].Status().Code)
			require.Equal(t, testCase.expectDescription, spans[0].Status().Description)
			require.Contains(t, spans[0].Attributes(), attribute.String("error.type", testCase.expectType))
		})
	}
}

func TestRecoverPanic(t *testing.T) {
	t.Parallel()

	t.Run("AbsorbsAPanic", func(t *testing.T) {
		t.Parallel()

		done := make(chan struct{})

		// A missed panic ends the test binary; reaching done is the assertion.
		go func() {
			defer close(done)

			_, span := otel.Tracer().Start(t.Context(), "test.RecoverPanic")
			defer span.End()
			defer otel.RecoverPanic(t.Context(), span)

			panic("boom")
		}()

		<-done
	})

	t.Run("NoOpWithoutAPanic", func(t *testing.T) {
		t.Parallel()

		_, span := otel.Tracer().Start(t.Context(), "test.RecoverPanic")
		defer span.End()

		otel.RecoverPanic(t.Context(), span)
	})
}

// panicDetached panics with a value that holds caller input, on a goroutine RecoverPanic guards.
func panicDetached(ctx context.Context, span trace.Span) {
	defer otel.RecoverPanic(ctx, span)

	panic("secret")
}

//nolint:paralleltest // Records through the process-wide logger provider.
func TestRecoverPanicFaults(t *testing.T) {
	exporter := &recordingExporter{}
	gootel.SetLoggerProvider(sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter))))
	t.Cleanup(func() { gootel.SetLoggerProvider(lognoop.NewLoggerProvider()) })

	provider := sdktrace.NewTracerProvider()

	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

	// RecoverPanic takes the context of the span's parent, as a goroutine that starts its span does.
	_, span := provider.Tracer("otel-test").Start(t.Context(), "detached")
	defer span.End()

	panicDetached(t.Context(), span)

	// One log record, linked to the span, holds the panic's type and the stack it began on.
	require.Len(t, exporter.records, 1)

	record := exporter.records[0]
	require.Equal(t, log.SeverityError, record.Severity())
	require.Equal(t, span.SpanContext().TraceID(), record.TraceID())
	require.Equal(t, span.SpanContext().SpanID(), record.SpanID())

	var attributes []attribute.KeyValue

	record.WalkAttributes(func(kv attribute.KeyValue) bool {
		attributes = append(attributes, kv)

		return true
	})

	attributeSet := attribute.NewSet(attributes...)
	exceptionType, _ := attributeSet.Value("exception.type")
	stacktrace, _ := attributeSet.Value("exception.stacktrace")

	require.Equal(t, "string", exceptionType.AsString())
	require.Contains(t, stacktrace.AsString(), "otel_test.panicDetached")
}

//nolint:paralleltest // Swaps the process-wide tracer provider.
func TestForceFlush(t *testing.T) {
	t.Run("Success/ExportsBufferedSpans", func(t *testing.T) {
		exporter := tracetest.NewInMemoryExporter()
		provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Hour)))

		t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

		gootel.SetTracerProvider(provider)
		t.Cleanup(func() { gootel.SetTracerProvider(noop.NewTracerProvider()) })

		_, span := otel.Tracer().Start(t.Context(), "buffered")
		span.End()

		require.Empty(t, exporter.GetSpans())
		require.NoError(t, otel.ForceFlush(t.Context()))
		require.Len(t, exporter.GetSpans(), 1)
	})

	t.Run("Success/NothingToFlush", func(t *testing.T) {
		require.NoError(t, otel.ForceFlush(t.Context()))
	})
}
