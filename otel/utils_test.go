package otel_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

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
