package logging

import "google.golang.org/grpc"

// RPCConfig produces the gRPC server interceptors a service installs. They log each unary and
// streaming call, and turn a handler panic into an internal error. The presets subpackage
// implements it for a terminal and for Google Cloud.
type RPCConfig interface {
	// UnaryInterceptor logs each unary call.
	UnaryInterceptor() grpc.UnaryServerInterceptor
	// StreamInterceptor logs each streaming call.
	StreamInterceptor() grpc.StreamServerInterceptor
	// PanicUnaryInterceptor recovers from a panic in a unary handler, logging it
	// and returning an internal error to the caller.
	PanicUnaryInterceptor() grpc.UnaryServerInterceptor
	// PanicStreamInterceptor recovers from a panic in a streaming handler.
	PanicStreamInterceptor() grpc.StreamServerInterceptor
}
