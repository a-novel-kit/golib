package logging

import "google.golang.org/grpc"

// RPCConfig produces the gRPC server interceptors a service installs. They log each unary and
// streaming call, and turn a handler panic into an internal error. The presets subpackage
// implements it for a terminal, and for an OpenTelemetry backend, where the RPC span records each
// call instead of a log.
type RPCConfig interface {
	// UnaryInterceptor logs each unary call, unless its RPC span is its record.
	UnaryInterceptor() grpc.UnaryServerInterceptor
	// StreamInterceptor logs each streaming call, unless its RPC span is its record.
	StreamInterceptor() grpc.StreamServerInterceptor
	// PanicUnaryInterceptor recovers from a panic in a unary handler, logging it
	// and returning an internal error to the caller.
	PanicUnaryInterceptor() grpc.UnaryServerInterceptor
	// PanicStreamInterceptor recovers from a panic in a streaming handler.
	PanicStreamInterceptor() grpc.StreamServerInterceptor
}
