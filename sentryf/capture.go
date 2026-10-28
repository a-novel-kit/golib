package sentryf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/internal/errtype"
)

// SendTimeout bounds one event's delivery to Sentry. A process can start before its network: a fresh
// Cloud Run instance's first connection out waits 30 seconds or more for Cloud NAT. TCP retries the
// connection meanwhile, so an event sent then arrives late. A job's [Flush] outlasts this timeout.
const SendTimeout = 2 * time.Minute

// Init installs the reporter config describes, process-wide, so a capture point needs no handle to
// it. Without a DSN, the capture functions send nothing and no connection opens.
//
// With a DSN, Init also starts the sampler, which sends the resource use of the process every minute.
// Each call stops the sampler the previous call started.
func Init(config Config) error {
	stopSampler()

	if config.DSN == "" {
		sentry.CurrentHub().BindClient(nil)

		return nil
	}

	if config.Release == "" || config.Environment == "" {
		return errors.New("sentry release and environment are required with a DSN")
	}

	transport := config.Transport
	if transport == nil {
		httpTransport := sentry.NewHTTPTransport()
		httpTransport.Timeout = SendTimeout
		transport = httpTransport
	}

	// The zero values keep tracing off and send every error event.
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:         config.DSN,
		Release:     releaseName(config.Release),
		Environment: config.Environment,
		Transport:   transport,
		BeforeSend:  dropVisitorData,
	})
	if err != nil {
		return fmt.Errorf("create sentry client: %w", err)
	}

	sentry.CurrentHub().BindClient(client)

	stopSampler = startSampler(sentry.NewMeter(context.Background()), os.DirFS(cgroupRoot), sampleInterval)

	return nil
}

// Flush waits up to timeout for buffered events to reach Sentry. Defer it in main, so the event of a
// failing job leaves before the process exits. A job's timeout exceeds [SendTimeout], so an event it
// sent before its network came up still leaves. A server's fits its platform's shutdown grace.
func Flush(timeout time.Duration) {
	sentry.Flush(timeout)
}

// Capture sends err as a server fault of operation, such as the route "GET /v2/session/" or the name
// of a job. A job's main calls it with its failure.
//
// Sentry groups the events by operation and the type of the innermost error err wraps, since a
// wrapped error carries no stack trace. The event holds the trace and span IDs of the span in ctx.
func Capture(ctx context.Context, operation string, err error) {
	root := errtype.Innermost(err)

	event := newEvent(ctx, sentry.Exception{Type: root, Value: operation})
	event.Fingerprint = []string{operation, root}

	sentry.CurrentHub().Client().CaptureEvent(event, nil, nil)
}

// CapturePanic sends rec, a recovered panic, as a server fault. Call it from the deferred function
// that recovered rec: the event then holds the stack of the panic, which Sentry groups it by. The
// event names the type of rec, never its value.
func CapturePanic(ctx context.Context, rec any) {
	event := newEvent(ctx, sentry.Exception{
		Type:       fmt.Sprintf("%T", rec),
		Value:      "recovered panic",
		Stacktrace: sentry.NewStacktrace(),
	})

	sentry.CurrentHub().Client().CaptureEvent(event, nil, nil)
}

// releaseName returns release with the leading "v" of its version dropped, as release CI names the
// release in Sentry.
func releaseName(release string) string {
	service, version, found := strings.Cut(release, "@")
	if !found {
		return release
	}

	return service + "@" + strings.TrimPrefix(version, "v")
}

// newEvent returns an error event holding exception and the trace context of the span in ctx.
func newEvent(ctx context.Context, exception sentry.Exception) *sentry.Event {
	event := sentry.NewEvent()
	event.Level = sentry.LevelError
	event.Exception = []sentry.Exception{exception}

	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		event.Contexts["trace"] = sentry.Context{
			"trace_id": spanContext.TraceID().String(),
			"span_id":  spanContext.SpanID().String(),
		}
	}

	return event
}

// dropVisitorData strips the request and the user from event. It keeps an SDK default from
// attaching either.
func dropVisitorData(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	event.Request = nil
	event.User = sentry.User{}

	return event
}
