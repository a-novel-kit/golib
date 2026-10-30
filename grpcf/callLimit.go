package grpcf

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
)

// callsInFlightMetric opens the gauge a [CallLimit] serves, in the Prometheus text format. The count
// follows it on the same line.
const callsInFlightMetric = "# HELP grpc_server_calls_in_flight The gRPC calls the server runs now.\n" +
	"# TYPE grpc_server_calls_in_flight gauge\n" +
	"grpc_server_calls_in_flight "

// CallLimit is a server option that caps the calls a server runs at once across all connections,
// so the memory their requests hold stays bounded. The health service stays open and uncounted, so
// probes still reach a busy server. Pass it to [NewServer] as an extra option. It sets the server's
// tap handle, of which gRPC allows one.
//
// The server refuses a call from its headers, before reading the request, with Unavailable: the
// client may retry it, here or on another instance. The refusal reaches no interceptor, so only the
// client records it. A call holds its slot until it ends or its client cancels it, and a stream
// holds one for as long as it stays open. [MethodCallLimit] adds a cap of its own to one method.
//
// CallLimit is also an [http.Handler] that serves the calls in flight as the gauge
// grpc_server_calls_in_flight, in the Prometheus text format. An autoscaler that reads the gauge adds
// instances before they fill. Serve it on a port of its own, beside the gRPC server.
type CallLimit struct {
	grpc.ServerOption

	server  callCount
	methods map[string]*callCount
}

// CallLimitOption configures a [CallLimit].
type CallLimitOption func(*CallLimit)

// MethodCallLimit caps the calls to one method that run at once, inside the server's cap: each call
// takes a slot from both. It suits a method far costlier than the others, so its calls cannot fill
// the server and starve the cheap ones. Name the method in full, as "/package.Service/Method". A
// limit of zero or less bounds nothing.
func MethodCallLimit(fullMethod string, limit int) CallLimitOption {
	return func(callLimit *CallLimit) {
		callLimit.methods[fullMethod] = &callCount{limit: int64(limit)}
	}
}

// NewCallLimit returns a CallLimit that runs limit calls at once. A limit of zero or less bounds
// nothing, and the calls in flight are still counted.
func NewCallLimit(limit int, options ...CallLimitOption) *CallLimit {
	callLimit := &CallLimit{server: callCount{limit: int64(limit)}, methods: map[string]*callCount{}}
	for _, option := range options {
		option(callLimit)
	}

	callLimit.ServerOption = grpc.InTapHandle(callLimit.admit)

	return callLimit
}

// ServeHTTP answers any request with the calls in flight.
func (callLimit *CallLimit) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	// A write fails only once the scraper has left, and its next scrape reads the count again.
	_, _ = io.WriteString(w, callsInFlightMetric+strconv.FormatInt(callLimit.server.inFlight.Load(), 10)+"\n")
}

// admit counts the call info describes until it ends, or refuses it with Unavailable once its method
// or the server runs its limit of calls.
func (callLimit *CallLimit) admit(ctx context.Context, info *tap.Info) (context.Context, error) {
	if strings.HasPrefix(info.FullMethodName, healthMethods) {
		return ctx, nil
	}

	// The method's cap comes first, so a full method takes no slot from the server.
	method := callLimit.methods[info.FullMethodName]
	if method != nil && !method.take() {
		return ctx, status.Error(codes.Unavailable, "method at its concurrent call limit: retry later")
	}

	if !callLimit.server.take() {
		method.release()

		return ctx, status.Error(codes.Unavailable, "server at its concurrent call limit: retry later")
	}

	// The transport cancels the call's context once the call ends, however it ends.
	context.AfterFunc(ctx, func() {
		callLimit.server.release()
		method.release()
	})

	return ctx, nil
}

// callCount counts calls in flight against a limit, which bounds nothing at zero or less.
type callCount struct {
	limit    int64
	inFlight atomic.Int64
}

// take counts one more call in flight, unless limit calls already run.
func (count *callCount) take() bool {
	for {
		inFlight := count.inFlight.Load()
		if count.limit > 0 && inFlight >= count.limit {
			return false
		}

		if count.inFlight.CompareAndSwap(inFlight, inFlight+1) {
			return true
		}
	}
}

// release counts one call fewer. A nil count, for a method without a cap of its own, does nothing.
func (count *callCount) release() {
	if count != nil {
		count.inFlight.Add(-1)
	}
}

// MaxConcurrentCalls returns a server option that refuses a call while limit others run, as
// [CallLimit] describes.
//
// Deprecated: Use [NewCallLimit], whose CallLimit also serves the calls in flight.
func MaxConcurrentCalls(limit int) grpc.ServerOption {
	return NewCallLimit(limit)
}
