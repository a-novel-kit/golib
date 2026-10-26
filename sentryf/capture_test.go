package sentryf_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/require"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
	"github.com/a-novel-kit/golib/sentryf"
	"github.com/a-novel-kit/golib/sentryf/sentryftest"
)

// testDSN names a project on a reserved host, which no test contacts.
const testDSN = "https://public@sentry.invalid/1"

// driverError stands for an error type of a dependency, whose message holds caller input.
type driverError struct{}

func (*driverError) Error() string {
	return "password authentication failed for user=secret"
}

// stalledTransport keeps events in memory and never finishes a flush before its deadline.
type stalledTransport struct {
	sentry.MockTransport
}

func (*stalledTransport) FlushWithContext(ctx context.Context) bool {
	<-ctx.Done()

	return false
}

// newTracer returns a tracer of a real provider, so the spans it starts have a valid context.
func newTracer(t *testing.T) trace.Tracer {
	t.Helper()

	provider := sdktrace.NewTracerProvider()

	t.Cleanup(func() {
		err := provider.Shutdown(context.WithoutCancel(t.Context()))
		if err != nil {
			panic(err)
		}
	})

	return provider.Tracer("sentryf-test")
}

// requireAbsent fails t when event, as Sentry receives it, holds one of words.
func requireAbsent(t *testing.T, event *sentry.Event, words ...string) {
	t.Helper()

	payload, err := json.Marshal(event)
	require.NoError(t, err)

	for _, word := range words {
		require.NotContains(t, string(payload), word)
	}
}

//nolint:paralleltest // Installs the process-wide Sentry reporter.
func TestInit(t *testing.T) {
	testCases := []struct {
		name string

		config sentryf.Config

		expectErr     bool
		expectEnabled bool
	}{
		{
			name: "Success",

			config: sentryf.Config{DSN: testDSN, Release: "golib@1.0.0", Environment: "test"},

			expectEnabled: true,
		},
		{
			// A service and its local runs report nothing until they get a DSN.
			name: "Success/NoDSN",

			config: sentryf.Config{Release: "golib@1.0.0", Environment: "test"},
		},
		{
			name: "Error/NoRelease",

			config: sentryf.Config{DSN: testDSN, Environment: "test"},

			expectErr: true,
		},
		{
			name: "Error/NoEnvironment",

			config: sentryf.Config{DSN: testDSN, Release: "golib@1.0.0"},

			expectErr: true,
		},
		{
			name: "Error/InvalidDSN",

			config: sentryf.Config{DSN: "sentry.invalid", Release: "golib@1.0.0", Environment: "test"},

			expectErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			transport := &sentry.MockTransport{}
			config := testCase.config
			config.Transport = transport

			t.Cleanup(func() { require.NoError(t, sentryf.Init(sentryf.Config{})) })

			err := sentryf.Init(config)
			if testCase.expectErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.Equal(t, testCase.expectEnabled, sentry.CurrentHub().Client().IsEnabled())

			sentryf.Capture(t.Context(), "rotate-keys", &driverError{})

			if !testCase.expectEnabled {
				require.Empty(t, transport.Events())

				return
			}

			require.Len(t, transport.Events(), 1)
		})
	}

	t.Run("Success/DropsVisitorData", func(t *testing.T) {
		transport := sentryftest.Record(t)

		// An SDK default could attach the request and the user from a scope.
		request := httptest.NewRequestWithContext(
			t.Context(), http.MethodPost, "/v2/session?token=secret", strings.NewReader(`{"password":"secret"}`),
		)
		request.RemoteAddr = "203.0.113.7:41234"
		request.Header.Set("Authorization", "Bearer secret")
		request.Header.Set("Cookie", "session=secret")

		hub := sentry.CurrentHub().Clone()
		hub.Scope().SetRequest(request)
		hub.Scope().SetUser(sentry.User{IPAddress: "203.0.113.7", Email: "secret@example.com"})
		hub.CaptureMessage("fault")

		events := transport.Events()
		require.Len(t, events, 1)
		require.Nil(t, events[0].Request)
		require.Equal(t, sentry.User{}, events[0].User)
		requireAbsent(t, events[0], "secret", "203.0.113.7")
	})
}

//nolint:paralleltest // Installs the process-wide Sentry reporter.
func TestFlush(t *testing.T) {
	testCases := []struct {
		name string

		transport sentry.Transport
		timeout   time.Duration

		expectMin time.Duration
		expectMax time.Duration
	}{
		{
			// The flush returns as soon as the transport drains.
			name: "Success/Drained",

			transport: &sentry.MockTransport{},
			timeout:   time.Minute,

			expectMax: 5 * time.Second,
		},
		{
			// A transport that never drains holds the flush to its timeout, and no longer.
			name: "Error/Stalled",

			transport: &stalledTransport{},
			timeout:   50 * time.Millisecond,

			expectMin: 50 * time.Millisecond,
			expectMax: 5 * time.Second,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			config := sentryf.Config{
				DSN: testDSN, Release: "golib@1.0.0", Environment: "test", Transport: testCase.transport,
			}
			require.NoError(t, sentryf.Init(config))
			t.Cleanup(func() { require.NoError(t, sentryf.Init(sentryf.Config{})) })

			start := time.Now()

			sentryf.Flush(testCase.timeout)

			elapsed := time.Since(start)
			require.GreaterOrEqual(t, elapsed, testCase.expectMin)
			require.Less(t, elapsed, testCase.expectMax)
		})
	}
}

//nolint:paralleltest // Records through the process-wide Sentry reporter.
func TestCapture(t *testing.T) {
	errSentinel := errors.New("secret sentinel")
	errReported := otel.ReportError(trace.SpanFromContext(context.Background()), &driverError{})

	testCases := []struct {
		name string

		operation string
		err       error
		inSpan    bool

		expectType string
	}{
		{
			name: "Success/Wrapped",

			operation: "GET /v2/session/",
			err:       fmt.Errorf("select session: %w", &driverError{}),
			inSpan:    true,

			expectType: "*sentryf_test.driverError",
		},
		{
			// golib's own marks, such as a span's report, wrap the cause.
			name: "Success/Reported",

			operation: "GET /v2/session/",
			err:       fmt.Errorf("select session: %w", errReported),
			inSpan:    true,

			expectType: "*sentryf_test.driverError",
		},
		{
			name: "Success/JoinedFollowsFirst",

			operation: "/jsonkeys.JwkGetService/JwkGet",
			err:       errors.Join(fmt.Errorf("select jwk: %w", &driverError{}), errSentinel),
			inSpan:    true,

			expectType: "*sentryf_test.driverError",
		},
		{
			// A job's main reports its failure under the job's name.
			name: "Success/Job",

			operation: "rotate-keys",
			err:       fmt.Errorf("rotate keys: %w", errSentinel),

			expectType: "*errors.errorString",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			transport := sentryftest.Record(t)

			ctx := t.Context()

			var span trace.Span
			if testCase.inSpan {
				ctx, span = newTracer(t).Start(ctx, "operation")
				defer span.End()
			}

			sentryf.Capture(ctx, testCase.operation, testCase.err)

			events := transport.Events()
			require.Len(t, events, 1)

			event := events[0]
			require.Equal(t, sentry.LevelError, event.Level)
			require.Equal(t, sentryftest.Release, event.Release)
			require.Equal(t, sentryftest.Environment, event.Environment)
			require.Equal(t, []string{testCase.operation, testCase.expectType}, event.Fingerprint)
			require.Equal(t, []sentry.Exception{{Type: testCase.expectType, Value: testCase.operation}}, event.Exception)
			require.Nil(t, event.Request)
			require.Equal(t, sentry.User{}, event.User)
			requireAbsent(t, event, "secret")

			if !testCase.inSpan {
				require.NotContains(t, event.Contexts, "trace")

				return
			}

			require.Equal(t, sentry.Context{
				"trace_id": span.SpanContext().TraceID().String(),
				"span_id":  span.SpanContext().SpanID().String(),
			}, event.Contexts["trace"])
		})
	}
}

//nolint:paralleltest // Records through the process-wide Sentry reporter.
func TestCapturePanic(t *testing.T) {
	testCases := []struct {
		name string

		value any

		expectType string
	}{
		{
			name: "String",

			value: "secret",

			expectType: "string",
		},
		{
			name: "Error",

			value: errors.New("secret"),

			expectType: "*errors.errorString",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			transport := sentryftest.Record(t)

			ctx, span := newTracer(t).Start(t.Context(), "operation")
			defer span.End()

			func() {
				defer func() { sentryf.CapturePanic(ctx, recover()) }()

				panicWith(testCase.value)
			}()

			events := transport.Events()
			require.Len(t, events, 1)

			event := events[0]
			require.Len(t, event.Exception, 1)
			require.Equal(t, testCase.expectType, event.Exception[0].Type)
			require.Equal(t, "recovered panic", event.Exception[0].Value)
			// Sentry groups a panic by its stack, which ends where the panic began.
			require.Empty(t, event.Fingerprint)

			frames := event.Exception[0].Stacktrace.Frames
			require.NotEmpty(t, frames)
			require.Equal(t, "panicWith", frames[len(frames)-1].Function)

			require.Equal(t, span.SpanContext().TraceID().String(), event.Contexts["trace"]["trace_id"])
			requireAbsent(t, event, "secret")
		})
	}
}

// panicWith panics with value, as the code a recovery guards.
func panicWith(value any) {
	panic(value)
}
