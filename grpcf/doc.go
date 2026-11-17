// Package grpcf holds the gRPC glue a service needs on both sides of the wire, as httpf does for
// HTTP.
//
// Inbound, it assembles a server from its GRPC_* configuration with the standard interceptor
// chain, and ListenAndServe runs it under a bounded shutdown, over TLS once the configuration names
// a key pair. Its service token gate admits only the services allowed to call, and its health
// service reports the server's state to probes and balancers. Outbound, it reads where a dependency
// lives from its <PREFIX>_* configuration, and dials it as a local target or a service in the same
// Kubernetes cluster.
package grpcf
