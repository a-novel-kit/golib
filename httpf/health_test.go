package httpf_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/httpf"
	"github.com/a-novel-kit/golib/internal/oteltest"
)

var errProbeDown = errors.New("connection refused")

func probeUp(context.Context) error { return nil }

func probeDown(context.Context) error { return errProbeDown }

func TestPing(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	httpf.Ping(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ping", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "text/plain", recorder.Header().Get("Content-Type"))
	require.Equal(t, "pong", recorder.Body.String())
}

func TestHealth(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		probes map[string]func(context.Context) error

		expectStatus int
		expectBody   string
	}{
		{
			name: "Success/Up",

			probes: map[string]func(context.Context) error{"client:postgres": probeUp, "api:jsonKeys": probeUp},

			expectStatus: http.StatusOK,
			expectBody:   `{"api:jsonKeys": {"status": "up"}, "client:postgres": {"status": "up"}}`,
		},
		{
			// The body names the state alone: the error stays on the probe's span.
			name: "Error/OneDown",

			probes: map[string]func(context.Context) error{"client:postgres": probeUp, "api:jsonKeys": probeDown},

			expectStatus: http.StatusServiceUnavailable,
			expectBody:   `{"api:jsonKeys": {"status": "down"}, "client:postgres": {"status": "up"}}`,
		},
		{
			name: "Success/NoProbe",

			expectStatus: http.StatusOK,
			expectBody:   `{}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthcheck", nil)
			httpf.Health(testCase.probes)(recorder, request)

			require.Equal(t, testCase.expectStatus, recorder.Code)
			require.JSONEq(t, testCase.expectBody, recorder.Body.String())
		})
	}

	// Each probe waits for the other: run one after the other, the first would time out.
	t.Run("Success/ProbesRunAtOnce", func(t *testing.T) {
		t.Parallel()

		var arrived sync.WaitGroup

		arrived.Add(2)

		meet := func(ctx context.Context) error {
			arrived.Done()

			met := make(chan struct{})

			go func() { arrived.Wait(); close(met) }()

			select {
			case <-met:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		t.Cleanup(cancel)

		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/healthcheck", nil)
		httpf.Health(map[string]func(context.Context) error{"first": meet, "second": meet})(recorder, request)

		require.Equal(t, http.StatusOK, recorder.Code)
	})
}

//nolint:paralleltest // Swaps the process-wide tracer provider.
func TestHealthSpans(t *testing.T) {
	recorder := oteltest.RecordSpans(t)

	// A probe describes its dependency on its own span.
	describe := func(ctx context.Context) error {
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("smtp.failure_category", "timeout"))

		return errProbeDown
	}

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthcheck", nil)
	httpf.Health(map[string]func(context.Context) error{"client:smtp": describe})(httptest.NewRecorder(), request)

	spans := recorder.Ended()
	require.Len(t, spans, 2)

	probe, handler := spans[0], spans[1]
	require.Equal(t, "rest.Health(client:smtp)", probe.Name())
	require.Equal(t, codes.Error, probe.Status().Code)
	require.Equal(t, errProbeDown.Error(), probe.Status().Description)
	require.Contains(t, probe.Attributes(), attribute.String("smtp.failure_category", "timeout"))

	// The handler span fails too, and leaves the detail to the probe's span.
	require.Equal(t, "rest.Health", handler.Name())
	require.Equal(t, codes.Error, handler.Status().Code)
	require.Empty(t, handler.Status().Description)
}
