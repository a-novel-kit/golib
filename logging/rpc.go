package logging

import "google.golang.org/grpc"

// RPCConfig is implemented by anything that produces gRPC interceptors
// emitting structured access logs (and a panic recovery handler).
type RPCConfig interface {
	UnaryInterceptor() grpc.UnaryServerInterceptor
	StreamInterceptor() grpc.StreamServerInterceptor
	PanicUnaryInterceptor() grpc.UnaryServerInterceptor
	PanicStreamInterceptor() grpc.StreamServerInterceptor
}

// RpcConfig is the legacy spelling of RPCConfig.
//
// Deprecated: use RPCConfig. The renamed alias matches the project's
// acronym-casing convention (`RPC`, not `Rpc`); behaviour is unchanged.
type RpcConfig = RPCConfig
