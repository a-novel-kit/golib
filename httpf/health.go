package httpf

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"
	"sync"

	"github.com/a-novel-kit/golib/otel"
)

// Ping answers the liveness probe with "pong", without touching any dependency. Mount it at the
// router's liveness path, which [NewMux] leaves untraced and never rate-limits.
func Ping(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)

	// A failed write means the prober left: there is nobody to answer.
	_, _ = io.WriteString(w, "pong")
}

// healthStatus is the state of one dependency in a health report. The route is public, so it carries
// the state alone: an error message can name internal hosts, ports or schemas. The error goes to the
// probe's span instead.
type healthStatus struct {
	// Status is "up" or "down".
	Status string `json:"status"`
}

// Health returns the handler of a health route. It runs every probe at once, each under a span named
// after it, and answers each probe's state under its name, such as
//
//	{"client:postgres": {"status": "up"}, "api:jsonKeys": {"status": "down"}}
//
// with 200 when every probe succeeds and 503 otherwise. A probe receives the context of its span, on
// which it may set attributes, and returns nil when its dependency is up, such as postgres.Health.
//
// A probe's error is exported on its span, as the span's status description: a probe returns an
// error safe to export, never a remote reply that can carry a credential, such as an SMTP server's.
//
// Health sets no timeout of its own. The request timeout of [NewMux] bounds it, and so does each
// probe's own, such as the second postgres.Health waits; a probe that ignores its context holds the
// answer until it returns.
func Health(probes map[string]func(ctx context.Context) error) http.HandlerFunc {
	names := slices.Sorted(maps.Keys(probes))

	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := otel.Tracer().Start(r.Context(), "rest.Health")
		defer span.End()

		errs := make([]error, len(names))

		var wg sync.WaitGroup

		for i, name := range names {
			wg.Go(func() { errs[i] = runProbe(ctx, name, probes[name]) })
		}

		wg.Wait()

		report := make(map[string]healthStatus, len(names))
		for i, name := range names {
			report[name] = healthStatus{Status: "up"}
			if errs[i] != nil {
				report[name] = healthStatus{Status: "down"}
			}
		}

		status := http.StatusOK

		err := errors.Join(errs...)
		if err != nil {
			_ = otel.ReportError(span, err)
			status = http.StatusServiceUnavailable
		}

		SendJSONStatus(w, span, status, report)
	}
}

// runProbe runs probe under a span named after the probe, and reports its error there.
func runProbe(ctx context.Context, name string, probe func(ctx context.Context) error) error {
	ctx, span := otel.Tracer().Start(ctx, "rest.Health("+name+")")
	defer span.End()

	err := probe(ctx)
	if err != nil {
		return otel.ReportError(span, err)
	}

	return nil
}
