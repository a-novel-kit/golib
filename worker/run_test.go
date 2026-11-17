package worker_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	otelglobal "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/a-novel-kit/golib/internal/oteltest"
	"github.com/a-novel-kit/golib/worker"
)

//nolint:paralleltest // Swaps the process-wide tracer provider.
func TestRun(t *testing.T) {
	errConnect := fmt.Errorf("connect to database: %w", &driverError{})

	testCases := []struct {
		name string

		fn func(context.Context) error

		expectErr  error
		expectType string
	}{
		{
			name: "Success",

			fn: func(context.Context) error { return nil },
		},
		{
			name: "Error/Dependency",

			fn: func(context.Context) error { return errConnect },

			expectErr:  errConnect,
			expectType: "*worker_test.driverError",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := oteltest.RecordSpans(t)

			// The process's own span: the job's run must not gather under it.
			ctx, process := otelglobal.GetTracerProvider().Tracer("worker-test").Start(t.Context(), "process")
			defer process.End()

			err := worker.Run(ctx, "job.Probe", testCase.fn)
			require.ErrorIs(t, err, testCase.expectErr)

			spans := recorder.Ended()
			require.Len(t, spans, 1)

			span := spans[0]
			require.Equal(t, "job.Probe", span.Name())
			require.False(t, span.Parent().IsValid(), "a job's run is the root of its trace")
			require.Equal(t, testCase.expectErr != nil, span.Status().Code == codes.Error)

			attributes := attribute.NewSet(span.Attributes()...)
			errorType, _ := attributes.Value("error.type")
			require.Equal(t, testCase.expectType, errorType.AsString())
		})
	}
}
