package loggingpresets

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/felixge/httpsnoop"

	"github.com/a-novel-kit/golib/logging"
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
//
// Deprecated: pass a nil logger to httpf.NewRouter. Under the otelpresets.OTLP backend, the server
// span is each request's record.
type HTTPGcloud struct {
	BaseLogger *LogGcloud
}

func (logger *HTTPGcloud) Logger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger.serve(next, w, r)
		})
	}
}

// serve runs next, then writes the access log entry of the request at the level its status grades.
func (logger *HTTPGcloud) serve(next http.Handler, w http.ResponseWriter, r *http.Request) {
	metrics := httpsnoop.CaptureMetrics(next, w, r)

	write := logger.BaseLogger.Info

	switch statusLevel(metrics.Code) {
	case LogLevelError:
		write = logger.BaseLogger.Err
	case LogLevelWarn:
		write = logger.BaseLogger.Warn
	}

	// The httpRequest group is the contract Cloud Logging reads to unpack the request.
	// https://docs.cloud.google.com/logging/docs/agent/logging/configuration#special-fields
	write(
		r.Context(),
		fmt.Sprintf("%s %s %d", r.Method, r.URL.Path, metrics.Code),
		slog.Group(
			"httpRequest",
			slog.String("requestMethod", r.Method),
			slog.String("requestUrl", r.URL.Path),
			slog.Int("status", metrics.Code),
			slog.Int64("requestSize", r.ContentLength),
			slog.String("remoteIp", r.RemoteAddr),
			slog.String("userAgent", r.UserAgent()),
			slog.String("referer", r.Referer()),
			slog.String("protocol", r.Proto),
			slog.String("latency", fmt.Sprintf("%.9fs", metrics.Duration.Seconds())),
			slog.String("responseSize", strconv.FormatInt(metrics.Written, 10)),
		),
	)
}
