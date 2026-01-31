package loggingpresets

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel/utils"
)

var _ logging.HttpConfig = (*HttpLocal)(nil)

type HttpLocal struct {
	BaseLogger LogLocal
}

func (logger *HttpLocal) Logger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrapped := &utils.CaptureHTTPResponseWriter{ResponseWriter: w}

			start := time.Now()

			next.ServeHTTP(wrapped, r)

			latency := time.Since(start)

			status := wrapped.Status()

			var (
				prefix string
				body   string
				logFn  func(ctx context.Context, msg string, fields ...any)
			)

			switch {
			case status >= http.StatusInternalServerError:
				logFn = logger.BaseLogger.Err
				prefix = "🗙 "
				body = string(wrapped.Response())
			case status >= http.StatusBadRequest:
				logFn = logger.BaseLogger.Warn
				prefix = "⚠ "
				body = string(wrapped.Response())
			default:
				logFn = logger.BaseLogger.Info
				prefix = "✓ "
				// Don't print body to keep logs clean.
			}

			lstyleExtra := logger.BaseLogger.Renderer.NewStyle().Faint(true)

			message := fmt.Sprintf("%s %s %s", prefix, r.Method, r.URL.Path)           // Path
			message += lstyleExtra.Render(fmt.Sprintf(" (%s)", latency))               // Latency
			message = lstyleExtra.Render(start.Format(time.StampNano)) + " " + message // Start time

			if body != "" {
				message += "\n\t" + strings.ReplaceAll(body, "\n", "\n\t")
			}

			logFn(r.Context(), strings.TrimSpace(message))
		})
	}
}
