package worker_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	otelglobal "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	"github.com/a-novel-kit/golib/sentryf/sentryftest"
	"github.com/a-novel-kit/golib/worker"
)

const (
	// pollTimeout bounds every wait below. The cases use an hour-long interval, so nothing here
	// times out for being slow.
	pollTimeout = time.Second

	// pollStagger is long enough to measure without making the suite wait on it.
	pollStagger = 150 * time.Millisecond

	// pollDrainRuns is how many consecutive units of work the drain case reports.
	pollDrainRuns = 5
)

// discardLogger keeps the loop's log lines out of the test output; no case asserts on them.
var discardLogger = &loggingpresets.LogLocal{Out: io.Discard}

// noWork is a run that finds nothing to do.
func noWork(context.Context) (bool, error) { return false, nil }

func TestPoll(t *testing.T) {
	t.Parallel()

	errFoo := errors.New("foo")

	t.Run("Success/ReturnsOnCancel", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		returned := make(chan struct{})

		go func() {
			defer close(returned)

			worker.Poll(ctx, discardLogger, "test", time.Hour, 0, noWork)
		}()

		cancel()

		select {
		case <-returned:
		case <-time.After(pollTimeout):
			require.FailNow(t, "Poll did not return on context cancellation")
		}
	})

	t.Run("Success/RerunsImmediatelyAfterWork", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var calls atomic.Int64

		drained := make(chan struct{})

		drain := func(context.Context) (bool, error) {
			count := calls.Add(1)
			if count < pollDrainRuns {
				return true, nil
			}

			if count == pollDrainRuns {
				close(drained)
			}

			return false, nil
		}

		// Reaching the last run inside pollTimeout is only possible if reporting work skips the
		// hour-long wait.
		go worker.Poll(ctx, discardLogger, "test", time.Hour, 0, drain)

		select {
		case <-drained:
		case <-time.After(pollTimeout):
			require.FailNow(t, "Poll waited out its interval instead of re-running after work")
		}
	})

	t.Run("Success/ContinuesAfterError", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var calls atomic.Int64

		recovered := make(chan struct{})

		failOnce := func(context.Context) (bool, error) {
			switch calls.Add(1) {
			case 1:
				return false, errFoo
			case 2:
				close(recovered)
			}

			return false, nil
		}

		go worker.Poll(ctx, discardLogger, "test", time.Millisecond, 0, failOnce)

		select {
		case <-recovered:
		case <-time.After(pollTimeout):
			require.FailNow(t, "Poll stopped after the callback returned an error")
		}
	})

	t.Run("Success/StaggersTheFirstRun", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		started := time.Now()
		firstCall := make(chan time.Duration, 1)

		recordFirstCall := func(context.Context) (bool, error) {
			select {
			case firstCall <- time.Since(started):
			default:
			}

			return false, nil
		}

		go worker.Poll(ctx, discardLogger, "test", time.Hour, pollStagger, recordFirstCall)

		select {
		case elapsed := <-firstCall:
			require.GreaterOrEqual(t, elapsed, pollStagger)
		case <-time.After(pollTimeout):
			require.FailNow(t, "Poll never ran its callback")
		}
	})
}

//nolint:paralleltest // Replaces the process-wide tracer provider while the loop runs.
func TestPollTelemetry(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otelglobal.GetTracerProvider()

	otelglobal.SetTracerProvider(provider)
	t.Cleanup(func() {
		otelglobal.SetTracerProvider(previous)
		require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context())))
	})

	// The process's own span: the runs must not gather under it.
	ctx, process := provider.Tracer("worker-test").Start(t.Context(), "process")
	defer process.End()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var calls atomic.Int64

	returned := make(chan struct{})

	go func() {
		defer close(returned)

		// The first run reports work, so the second follows at once and stops the loop.
		worker.Poll(ctx, discardLogger, "telemetry", time.Hour, 0, func(context.Context) (bool, error) {
			if calls.Add(1) == 2 {
				cancel()

				return false, nil
			}

			return true, nil
		})
	}()

	select {
	case <-returned:
	case <-time.After(pollTimeout):
		require.FailNow(t, "Poll did not return")
	}

	// Two runs, and nothing else: no span outlives the loop.
	spans := recorder.Ended()
	require.Len(t, spans, 2)

	for _, span := range spans {
		require.Equal(t, "worker.Poll", span.Name())
		require.False(t, span.Parent().IsValid())
		require.NotEqual(t, process.SpanContext().TraceID(), span.SpanContext().TraceID())
	}

	require.NotEqual(t, spans[0].SpanContext().TraceID(), spans[1].SpanContext().TraceID())
}

// driverError stands for an error type of a dependency.
type driverError struct{}

func (*driverError) Error() string {
	return "connection refused"
}

//nolint:paralleltest // Records through the process-wide Sentry reporter and tracer provider.
func TestPollFaults(t *testing.T) {
	testCases := []struct {
		name string

		// fail ends the first run.
		fail func() error

		// expectEvent is the value of the one Sentry event the loop sends.
		expectEvent string
	}{
		{
			name: "Error/Fails",

			fail: func() error { return fmt.Errorf("sweep: %w", &driverError{}) },

			expectEvent: "test",
		},
		{
			name: "Error/Panics",

			fail: func() error { panic(fmt.Errorf("connect: %w", &driverError{})) },

			expectEvent: "recovered panic",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			transport := sentryftest.Record(t)

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			previous := otelglobal.GetTracerProvider()

			otelglobal.SetTracerProvider(provider)
			t.Cleanup(func() {
				otelglobal.SetTracerProvider(previous)
				require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context())))
			})

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			var calls atomic.Int64

			recovered := make(chan struct{})

			// Only a loop that survives the failed run reaches the second one.
			failOnce := func(context.Context) (bool, error) {
				switch calls.Add(1) {
				case 1:
					return false, testCase.fail()
				case 2:
					close(recovered)
				}

				return false, nil
			}

			go worker.Poll(ctx, discardLogger, "test", time.Millisecond, 0, failOnce)

			select {
			case <-recovered:
			case <-time.After(pollTimeout):
				require.FailNow(t, "Poll stopped after the failed run")
			}

			events := transport.Events()
			require.Len(t, events, 1)
			require.Equal(t, testCase.expectEvent, events[0].Exception[0].Value)

			// Only the failed run's span names a fault, by the innermost error's type.
			var errorTypes []string

			for _, span := range recorder.Ended() {
				attributes := attribute.NewSet(span.Attributes()...)

				errorType, ok := attributes.Value("error.type")
				if ok {
					errorTypes = append(errorTypes, errorType.AsString())
				}
			}

			require.Equal(t, []string{"*worker_test.driverError"}, errorTypes)
		})
	}
}
