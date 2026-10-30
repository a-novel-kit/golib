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
// holds one for as long as it stays open.
//
// CallLimit is also an [http.Handler] that serves the calls in flight as the gauge
// grpc_server_calls_in_flight, in the Prometheus text format. An autoscaler that reads the gauge adds
// instances before they fill. Serve it on a port of its own, beside the gRPC server.
type CallLimit struct {
	grpc.ServerOption

	limit    int64
	inFlight atomic.Int64
}

// NewCallLimit returns a CallLimit that runs limit calls at once. A limit of zero or less bounds
// nothing, and the calls in flight are still counted.
func NewCallLimit(limit int) *CallLimit {
	callLimit := &CallLimit{limit: int64(limit)}
	callLimit.ServerOption = grpc.InTapHandle(callLimit.admit)

	return callLimit
}

// ServeHTTP answers any request with the calls in flight.
func (callLimit *CallLimit) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	// A write fails only once the scraper has left, and its next scrape reads the count again.
	_, _ = io.WriteString(w, callsInFlightMetric+strconv.FormatInt(callLimit.inFlight.Load(), 10)+"\n")
}

// admit counts the call info describes until it ends, or refuses it with Unavailable once limit
// calls run.
func (callLimit *CallLimit) admit(ctx context.Context, info *tap.Info) (context.Context, error) {
	if strings.HasPrefix(info.FullMethodName, healthMethods) {
		return ctx, nil
	}

	if !callLimit.take() {
		return ctx, status.Error(codes.Unavailable, "server at its concurrent call limit: retry later")
	}

	// The transport cancels the call's context once the call ends, however it ends.
	context.AfterFunc(ctx, func() { callLimit.inFlight.Add(-1) })

	return ctx, nil
}

// take counts one more call in flight, unless limit calls already run.
func (callLimit *CallLimit) take() bool {
	for {
		inFlight := callLimit.inFlight.Load()
		if callLimit.limit > 0 && inFlight >= callLimit.limit {
			return false
		}

		if callLimit.inFlight.CompareAndSwap(inFlight, inFlight+1) {
			return true
		}
	}
}

// MaxConcurrentCalls returns a server option that refuses a call while limit others run, as
// [CallLimit] describes.
//
// Deprecated: Use [NewCallLimit], whose CallLimit also serves the calls in flight.
func MaxConcurrentCalls(limit int) grpc.ServerOption {
	return NewCallLimit(limit)
}
