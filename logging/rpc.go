package logging

import "google.golang.org/grpc"

// RPCConfig produces the gRPC server interceptors a service installs: access
// logging for unary and streaming calls, plus panic recovery that turns a
// handler panic into an internal error. The presets subpackage implements it in
// a local and a Google Cloud variant.
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
