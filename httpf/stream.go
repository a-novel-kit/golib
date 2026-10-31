package httpf

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrInvalidEvent is returned by [EventStream.Send] for an event a client cannot read: one without
// data, or whose ID or type holds a line break or a NUL.
var ErrInvalidEvent = errors.New("invalid server-sent event")

// requestTimeoutKey holds the [requestTimeoutState] of a request, which [Streaming] lifts.
type requestTimeoutKey struct{}

// requestTimeoutState is the request timeout of one request.
type requestTimeoutState struct {
	// afterDisconnect calls f once the client disconnects, as [context.AfterFunc] does.
	afterDisconnect func(f func()) (stop func() bool)
	// lifted is set by [Streaming], whose own limit then bounds the request.
	lifted bool
}

// requestTimeout ends a request's context once timeout passes, and answers 504 Gateway Timeout when
// the handler returns past it. A route under [Streaming] lifts it.
func requestTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			untimed := r.Context()
			state := &requestTimeoutState{
				afterDisconnect: func(f func()) func() bool { return context.AfterFunc(untimed, f) },
			}

			ctx, cancel := context.WithTimeout(context.WithValue(r.Context(), requestTimeoutKey{}, state), timeout)
			defer cancel()

			next.ServeHTTP(w, r.WithContext(ctx))

			if !state.lifted && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				w.WriteHeader(http.StatusGatewayTimeout)
			}
		})
	}
}

// Streaming returns the middleware of a route that streams its response, such as an [EventStream].
// The route answers for up to limit, past the router's request timeout and the server's write
// timeout. The request still ends when the client disconnects.
//
// Mount it before any middleware that ends the request, such as [EndOnShutdown]: it replaces the
// context the router's timeout set.
func Streaming(limit time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// The stream keeps the request's values, and ends with the client's connection.
			state, timed := ctx.Value(requestTimeoutKey{}).(*requestTimeoutState)
			if timed {
				state.lifted = true
				ctx = context.WithoutCancel(ctx)
			}

			ctx, cancel := context.WithTimeout(ctx, limit)
			defer cancel()

			if timed {
				stop := state.afterDisconnect(cancel)
				defer stop()
			}

			// A writer that cannot move its deadline keeps the server's write timeout, which then cuts the
			// stream first.
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(limit))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Event is one server-sent event.
type Event struct {
	// ID is the event's position. A client that reconnects sends the last ID it received in the
	// Last-Event-ID header, so the stream resumes after it. Empty keeps the previous ID.
	ID string
	// Type names the event. Empty sends the default type, "message".
	Type string
	// Data is the event's content, usually JSON. A client drops an event without data.
	Data []byte
}

// An EventStream writes a response as server-sent events, the text/event-stream format of the HTML
// standard. Every event is flushed as it is sent. A comment goes out at each heartbeat, so proxies
// keep an idle stream open.
//
// Send and the heartbeat share the writer, under a lock.
type EventStream struct {
	mu         sync.Mutex
	writer     http.ResponseWriter
	controller *http.ResponseController
	stop       context.CancelFunc
	// stopped closes once the heartbeat has stopped writing.
	stopped chan struct{}
}

// NewEventStream answers 200 OK as an event stream, and starts its heartbeat. Close the stream
// before the handler returns. Call it on a writer that has written nothing yet: it returns an error
// when the writer cannot flush.
func NewEventStream(ctx context.Context, w http.ResponseWriter, heartbeat time.Duration) (*EventStream, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	controller := http.NewResponseController(w)

	err := controller.Flush()
	if err != nil {
		return nil, fmt.Errorf("flush stream headers: %w", err)
	}

	ctx, stop := context.WithCancel(ctx)
	stream := &EventStream{writer: w, controller: controller, stop: stop, stopped: make(chan struct{})}

	go stream.beat(ctx, heartbeat)

	return stream, nil
}

// Send writes event and flushes it.
func (stream *EventStream) Send(event Event) error {
	if len(event.Data) == 0 || strings.ContainsAny(event.ID+event.Type, "\r\n\x00") {
		return ErrInvalidEvent
	}

	var frame strings.Builder

	if event.ID != "" {
		frame.WriteString("id: " + event.ID + "\n")
	}

	if event.Type != "" {
		frame.WriteString("event: " + event.Type + "\n")
	}

	// Each line of the data takes a field of its own, and the client joins them back.
	data := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(string(event.Data))
	for line := range strings.SplitSeq(data, "\n") {
		frame.WriteString("data: " + line + "\n")
	}

	frame.WriteString("\n")

	return stream.write(frame.String())
}

// Close stops the heartbeat, and returns once it no longer writes. The response ends once the
// handler returns.
func (stream *EventStream) Close() {
	stream.stop()
	<-stream.stopped
}

// beat writes a comment every interval until ctx ends.
func (stream *EventStream) beat(ctx context.Context, interval time.Duration) {
	defer close(stream.stopped)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A failed beat means the client is gone; the next Send reports it.
			_ = stream.write(":\n\n")
		}
	}
}

// write sends text and flushes it.
func (stream *EventStream) write(text string) error {
	stream.mu.Lock()
	defer stream.mu.Unlock()

	_, err := stream.writer.Write([]byte(text))
	if err != nil {
		return fmt.Errorf("write event: %w", err)
	}

	err = stream.controller.Flush()
	if err != nil {
		return fmt.Errorf("flush event: %w", err)
	}

	return nil
}
