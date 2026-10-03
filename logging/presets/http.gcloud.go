package loggingpresets

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel/utils"
)

var _ logging.HTTPConfig = (*HTTPGcloud)(nil)

// HTTPGcloud implements [logging.HTTPConfig] for Google Cloud. Its middleware
// times each request and emits a structured access log through BaseLogger,
// which attaches it to the request's server span. The logged URL omits the
// query, whose values can identify a person.
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

			// The httpRequest group is the contract Cloud Logging reads to unpack the request.
			// https://docs.cloud.google.com/logging/docs/agent/logging/configuration#special-fields
			logFn(
				r.Context(),
				fmt.Sprintf("%s %s %d", r.Method, r.URL.Path, status),
				slog.Group(
					"httpRequest",
					slog.String("requestMethod", r.Method),
					slog.String("requestUrl", r.URL.Path),
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
