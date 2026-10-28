// Package grpcf holds the gRPC glue a service needs on both sides of the wire, as httpf does for
// HTTP.
//
// Inbound, it assembles a server from its GRPC_* configuration with the standard interceptor
// chain, and ListenAndServe runs it under a bounded shutdown. Its service token interceptors admit
// only the services allowed to call. Outbound, it supplies the dial credentials of a local target, a
// Google Cloud target or a service in the same Kubernetes cluster.
package grpcf
