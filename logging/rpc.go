package logging

import "google.golang.org/grpc"

// RPCConfig is implemented by anything that produces gRPC interceptors —
// both for emitting structured access logs (Unary/Stream Interceptor) and
// for recovering from handler panics (PanicUnary/PanicStream Interceptor).
type RPCConfig interface {
	UnaryInterceptor() grpc.UnaryServerInterceptor
	StreamInterceptor() grpc.StreamServerInterceptor
	PanicUnaryInterceptor() grpc.UnaryServerInterceptor
	PanicStreamInterceptor() grpc.StreamServerInterceptor
}
