// Package grpcf holds the gRPC glue a service needs on both sides of the wire, as httpf does for
// HTTP.
//
// Inbound, it assembles a server from its GRPC_* configuration with the standard interceptor
// chain, and ListenAndServe runs it under a bounded shutdown. Outbound, it supplies the dial
// credentials of a local or a Google Cloud target.
package grpcf
