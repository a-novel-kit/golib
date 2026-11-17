package worker_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	otelglobal "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/a-novel-kit/golib/internal/oteltest"
	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	"github.com/a-novel-kit/golib/worker"
)

const (
	// pollTimeout bounds every wait below. The cases use an hour-long interval, so nothing here
	// times out for being slow.
	pollTimeout = time.Second

	// pollStagger delays the first run of the stagger case, on the fake clock of its bubble.
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

	// The bubble's fake clock makes the stagger exact, and costs the suite no wait.
	t.Run("Success/StaggersTheFirstRun", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			started := time.Now()

			var firstCall time.Duration

			recordFirstCall := func(context.Context) (bool, error) {
				if firstCall == 0 {
					firstCall = time.Since(started)
				}

				return false, nil
			}

			go worker.Poll(ctx, discardLogger, "test", time.Hour, pollStagger, recordFirstCall)

			time.Sleep(pollStagger)
			synctest.Wait()

			require.Equal(t, pollStagger, firstCall)
		})
	})
}

//nolint:paralleltest // Replaces the process-wide tracer provider while the loop runs.
func TestPollTelemetry(t *testing.T) {
	recorder := oteltest.RecordSpans(t)

	// The process's own span: the runs must not gather under it.
	ctx, process := otelglobal.GetTracerProvider().Tracer("worker-test").Start(t.Context(), "process")
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

//nolint:paralleltest // Swaps the process-wide tracer provider.
func TestPollFaults(t *testing.T) {
	testCases := []struct {
		name string

		// fail ends the first run.
		fail func() error
	}{
		{
			name: "Error/Fails",

			fail: func() error { return fmt.Errorf("sweep: %w", &driverError{}) },
		},
		{
			name: "Error/Panics",

			fail: func() error { panic(fmt.Errorf("connect: %w", &driverError{})) },
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := oteltest.RecordSpans(t)

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
