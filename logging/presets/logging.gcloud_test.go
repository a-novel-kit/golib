package loggingpresets_test

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
)

// gcloudEntry holds the fields of a LogGcloud entry that Cloud Logging reads.
type gcloudEntry struct {
	Msg          string `json:"msg"`
	Severity     string `json:"severity"`
	Trace        string `json:"logging.googleapis.com/trace"`
	SpanID       string `json:"logging.googleapis.com/spanId"`
	TraceSampled *bool  `json:"logging.googleapis.com/trace_sampled"`
}

func decodeGcloudEntry(t *testing.T, out *bytes.Buffer) gcloudEntry {
	t.Helper()

	var entry gcloudEntry

	err := json.Unmarshal(out.Bytes(), &entry)
	if err != nil {
		panic(err)
	}

	return entry
}

func TestLogGcloud(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		log    func(logger *loggingpresets.LogGcloud, ctx context.Context)
		inSpan bool

		expectSeverity string
	}{
		{
			name:           "Info",
			log:            func(logger *loggingpresets.LogGcloud, ctx context.Context) { logger.Info(ctx, "message") },
			expectSeverity: "INFO",
		},
		{
			name:           "Warn",
			log:            func(logger *loggingpresets.LogGcloud, ctx context.Context) { logger.Warn(ctx, "message") },
			expectSeverity: "WARNING",
		},
		{
			name:           "Err",
			log:            func(logger *loggingpresets.LogGcloud, ctx context.Context) { logger.Err(ctx, "message") },
			expectSeverity: "ERROR",
		},
		{
			name:           "Err/InSpan",
			log:            func(logger *loggingpresets.LogGcloud, ctx context.Context) { logger.Err(ctx, "message") },
			inSpan:         true,
			expectSeverity: "ERROR",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			var span trace.Span

			if testCase.inSpan {
				provider := sdktrace.NewTracerProvider()

				t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

				ctx, span = provider.Tracer("presets-test").Start(ctx, "operation")
				defer span.End()
			}

			out := &bytes.Buffer{}
			testCase.log(&loggingpresets.LogGcloud{ProjectId: "project", Out: out}, ctx)

			entry := decodeGcloudEntry(t, out)
			require.Equal(t, "message", entry.Msg)
			require.Equal(t, testCase.expectSeverity, entry.Severity)

			if !testCase.inSpan {
				require.Empty(t, entry.Trace)
				require.Empty(t, entry.SpanID)
				require.Nil(t, entry.TraceSampled)

				return
			}

			spanContext := span.SpanContext()
			require.Equal(t, "projects/project/traces/"+spanContext.TraceID().String(), entry.Trace)
			require.Equal(t, spanContext.SpanID().String(), entry.SpanID)
			require.Equal(t, spanContext.IsSampled(), *entry.TraceSampled)
		})
	}

	t.Run("Concurrent", func(t *testing.T) {
		t.Parallel()

		// One logger serves every request; the race detector checks it holds no lazy state.
		logger := &loggingpresets.LogGcloud{ProjectId: "project", Out: &lockedBuffer{}}

		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() { logger.Warn(t.Context(), "message") })
		}

		wg.Wait()
	})
}

// lockedBuffer is a bytes.Buffer safe for concurrent writers.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (buffer *lockedBuffer) Write(p []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	return buffer.buf.Write(p)
}
