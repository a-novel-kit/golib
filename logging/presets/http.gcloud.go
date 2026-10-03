package loggingpresets

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel/utils"
)

var _ logging.HTTPConfig = (*HTTPGcloud)(nil)

// HTTPGcloud implements [logging.HTTPConfig] for Google Cloud. Its middleware
// times each request and emits a structured access log whose fields Cloud
// Logging correlates with the request's server span. It logs through
// BaseLogger.
//
// Install it inside the OpenTelemetry HTTP middleware, which opens that span.
// HTTPGcloud passes the request on unchanged, so the server span takes its name
// from the route the router matches.
type HTTPGcloud struct {
	BaseLogger *LogGcloud
}

func (logger *HTTPGcloud) Logger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrapped := &utils.CaptureHTTPResponseWriter{ResponseWriter: w}

			start := time.Now()

			next.ServeHTTP(wrapped, r)

			latency := time.Since(start)
			status := wrapped.Status()

			var logFn func(ctx context.Context, msg string, fields ...any)

			switch {
			case status >= http.StatusInternalServerError:
				logFn = logger.BaseLogger.Err
			case status >= http.StatusBadRequest:
				logFn = logger.BaseLogger.Warn
			default:
				logFn = logger.BaseLogger.Info
			}

			spanCtx := trace.SpanContextFromContext(r.Context())
			traceID := spanCtx.TraceID().String()
			spanID := spanCtx.SpanID().String()
			traceSampled := spanCtx.IsSampled()

			// Cloud Logging links a log to its trace only when the trace is
			// given as this full project-scoped resource name.
			traceResource := fmt.Sprintf("projects/%s/traces/%s", logger.BaseLogger.ProjectId, traceID)

			// The magic field names below are the contract Cloud Logging reads
			// to correlate the entry with its trace and to unpack the request.
			// https://docs.cloud.google.com/logging/docs/structured-logging
			logFn(
				r.Context(),
				fmt.Sprintf("%s %s %d", r.Method, r.URL.Path, status),
				slog.String("logging.googleapis.com/trace", traceResource),
				slog.String("logging.googleapis.com/spanId", spanID),
				slog.Bool("logging.googleapis.com/trace_sampled", traceSampled),
				slog.Group(
					"httpRequest",
					slog.String("requestMethod", r.Method),
					slog.String("requestUrl", r.URL.String()),
					slog.Int("status", status),
					slog.Int64("requestSize", r.ContentLength),
					slog.String("remoteIp", r.RemoteAddr),
					slog.String("userAgent", r.UserAgent()),
					slog.String("referer", r.Referer()),
					slog.String("protocol", r.Proto),
					slog.String("latency", fmt.Sprintf("%.9fs", latency.Seconds())),
					slog.String("responseSize", strconv.FormatInt(wrapped.Size(), 10)),
				),
			)
		})
	}
}
